// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package clients_test

import (
	"context"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClientsService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Clients Service Suite")
}

var _ = Describe("clients.Service", func() {
	var svc *clients.Service

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		svc = clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
	})

	publicClient := func(id string) clients.CreateInput {
		return clients.CreateInput{
			ClientID:     id,
			ClientName:   "Test",
			ClientType:   "public",
			RedirectURIs: []string{"com.example://cb"},
			GrantTypes:   []string{"authorization_code", "refresh_token"},
			RequirePKCE:  utils.Ptr(true),
		}
	}

	Describe("Create", func() {
		It("creates a public client and returns no secret", func() {
			res, err := svc.Create(publicClient("rm_mobile"))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.ClientID).To(Equal("rm_mobile"))
			Expect(res.ClientSecret).To(BeEmpty(), "public clients have no secret")
			Expect(res.ClientType).To(Equal("public"))
		})

		It("creates a confidential client and returns the plaintext secret", func() {
			res, err := svc.Create(clients.CreateInput{
				ClientName: "Server", ClientType: "confidential",
				GrantTypes: []string{"authorization_code"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.ClientSecret).NotTo(BeEmpty(), "confidential clients get a secret")
		})

		It("auto-generates a client_id when none is supplied", func() {
			res, err := svc.Create(clients.CreateInput{ClientName: "X", ClientType: "public", RequirePKCE: utils.Ptr(true)})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.ClientID).To(HavePrefix("rm_"))
		})

		It("forces require_pkce=true for public clients even if false was sent (OAuth 2.1)", func() {
			in := publicClient("rm_x")
			in.RequirePKCE = utils.Ptr(false)
			_, err := svc.Create(in)
			Expect(err).NotTo(HaveOccurred())
			list, _ := svc.List(false)
			Expect(list[0].RequirePKCE).To(BeTrue())
		})

		It("rejects a wildcard redirect_uri (negative, exact-match rule)", func() {
			in := publicClient("rm_x")
			in.RedirectURIs = []string{"https://app.example.com/*"}
			_, err := svc.Create(in)
			Expect(err).To(HaveOccurred())
		})

		It("rejects an implicit/password/client_credentials grant (negative)", func() {
			for _, g := range []string{"implicit", "password", "client_credentials"} {
				in := publicClient("rm_" + g)
				in.GrantTypes = []string{g}
				_, err := svc.Create(in)
				Expect(err).To(HaveOccurred(), "grant %q must be rejected", g)
			}
		})

		It("rejects an unknown client_type (negative)", func() {
			in := publicClient("rm_x")
			in.ClientType = "spaceship"
			_, err := svc.Create(in)
			Expect(err).To(HaveOccurred())
		})

		It("rejects a duplicate client_id (negative, conditional create)", func() {
			_, err := svc.Create(publicClient("rm_dup"))
			Expect(err).NotTo(HaveOccurred())
			_, err = svc.Create(publicClient("rm_dup"))
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("List", func() {
		It("lists all created clients", func() {
			_, _ = svc.Create(publicClient("rm_a"))
			_, _ = svc.Create(publicClient("rm_b"))
			list, err := svc.List(false)
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(2))
		})

		It("omits the secret unless get_secret is true", func() {
			_, _ = svc.Create(clients.CreateInput{ClientID: "rm_c", ClientName: "S", ClientType: "confidential", GrantTypes: []string{"authorization_code"}})

			without, _ := svc.List(false)
			Expect(without[0].ClientSecret).To(BeEmpty(), "secret hidden by default")

			with, _ := svc.List(true)
			Expect(with[0].ClientSecret).NotTo(BeEmpty(), "get_secret returns the stored plaintext")
		})
	})

	Describe("Update", func() {
		It("replaces the mutable fields with the supplied full state", func() {
			_, _ = svc.Create(publicClient("rm_p"))
			got, err := svc.Update("rm_p", clients.UpdateInput{
				ClientName:   "Renamed",
				RedirectURIs: []string{"com.example://new"},
				GrantTypes:   []string{"authorization_code"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ClientName).To(Equal("Renamed"))
			Expect(got.RedirectURIs).To(Equal([]string{"com.example://new"}))
		})

		It("resets an omitted field to empty (full replace, not merge)", func() {
			_, _ = svc.Create(publicClient("rm_p"))
			// Update without redirect_uris — full-replace semantics blank it.
			got, err := svc.Update("rm_p", clients.UpdateInput{
				ClientName: "Renamed",
				GrantTypes: []string{"authorization_code"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(got.RedirectURIs).To(BeEmpty())
		})

		It("rejects a missing client_name (required)", func() {
			_, _ = svc.Create(publicClient("rm_p"))
			_, err := svc.Update("rm_p", clients.UpdateInput{GrantTypes: []string{"authorization_code"}})
			Expect(err).To(HaveOccurred())
		})

		It("rejects an update that would violate an invariant (negative, wildcard redirect)", func() {
			_, _ = svc.Create(publicClient("rm_p"))
			_, err := svc.Update("rm_p", clients.UpdateInput{ClientName: "X", RedirectURIs: []string{"https://x/*"}})
			Expect(err).To(HaveOccurred())
		})

		It("returns not-found for an unknown id (negative)", func() {
			_, err := svc.Update("nope", clients.UpdateInput{ClientName: "x"})
			Expect(err).To(MatchError(oauth_clients_db.ErrOAuthClientNotFound))
		})
	})

	Describe("AddRedirectURIs", func() {
		It("unions new URIs onto the existing set and dedups", func() {
			_, _ = svc.Create(publicClient("rm_r")) // seeds com.example://cb
			got, err := svc.AddRedirectURIs("rm_r", []string{"com.example://cb", "com.example://new"})
			Expect(err).NotTo(HaveOccurred())
			Expect(got.RedirectURIs).To(ConsistOf("com.example://cb", "com.example://new"))
		})

		It("returns not-found for an unknown id (negative)", func() {
			_, err := svc.AddRedirectURIs("nope", []string{"com.example://x"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Get", func() {
		It("returns a registered client without its secret", func() {
			_, _ = svc.Create(clients.CreateInput{ClientID: "rm_g", ClientName: "S", ClientType: "confidential", GrantTypes: []string{"authorization_code"}})
			got, err := svc.Get("rm_g")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ClientID).To(Equal("rm_g"))
			Expect(got.ClientSecret).To(BeEmpty(), "Get never returns the secret")
		})

		It("returns ErrClientNotFound for an unknown client (negative)", func() {
			_, err := svc.Get("ghost")
			Expect(err).To(MatchError(clients.ErrClientNotFound))
		})
	})

	Describe("Delete / IsRegistered", func() {
		It("hard-deletes the client so it is no longer registered", func() {
			_, _ = svc.Create(publicClient("rm_d"))
			registered, _ := svc.IsRegistered("rm_d")
			Expect(registered).To(BeTrue())

			Expect(svc.Delete("rm_d")).To(Succeed())

			registered, _ = svc.IsRegistered("rm_d")
			Expect(registered).To(BeFalse(), "a deleted client is gone")
			list, _ := svc.List(false)
			Expect(list).To(BeEmpty())
		})

		It("IsRegistered returns false (no error) for an unknown client", func() {
			registered, err := svc.IsRegistered("ghost")
			Expect(err).NotTo(HaveOccurred())
			Expect(registered).To(BeFalse())
		})
	})
})
