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

		// A redirect_uri is a URL this server sends a BROWSER to on a caller's say-so, so the
		// registry is the last place a mistake is cheap. Each entry below registers happily
		// under RFC 6749's bare "absolute URI, no fragment" rule and then either never matches
		// or leaks something.
		DescribeTable("refuses a redirect_uri that is not somewhere safe to send a browser (negative)",
			func(uri, why string) {
				in := publicClient("rm_ru")
				in.RedirectURIs = []string{uri}
				_, err := svc.Create(in)
				Expect(err).To(HaveOccurred(), why)

				// The same rule must hold for the post-logout list: it is the same kind of
				// destination reached the same way, and validating only one of the two is how
				// the weaker list becomes the one an attacker uses.
				in2 := publicClient("rm_ru2")
				in2.RedirectURIs = []string{"com.example://cb"}
				in2.PostLogoutRedirectURIs = []string{uri}
				_, err2 := svc.Create(in2)
				Expect(err2).To(HaveOccurred(), "post_logout_redirect_uris: "+why)
			},
			Entry("javascript:", "javascript:alert(1)", "names code to run, not a destination"),
			Entry("data:", "data:text/html,<script>alert(1)</script>", "renders attacker HTML on a redirect"),
			Entry("file:", "file:///etc/passwd", "a local file is never an OAuth callback"),
			Entry("a fragment", "https://app.example.com/cb#tok", "RFC 6749 s3.1.2: the response appends its own"),
			Entry("relative", "/callback", "not absolute: nothing says which host"),
			Entry("no host", "https:///callback", "https with an empty host never matches"),
			Entry("plain http off-loopback", "http://app.example.com/cb", "puts the authorization code on the wire in cleartext"),
			Entry("scheme with no destination", "myapp:", "addresses nothing, so it can never match"),
			Entry("blank", "   ", "a blank entry silently matches nothing"),
		)

		DescribeTable("admits the redirect_uri shapes real clients use",
			func(uri string) {
				in := publicClient("rm_ok")
				in.RedirectURIs = []string{uri}
				_, err := svc.Create(in)
				Expect(err).NotTo(HaveOccurred(), uri)
			},
			// Every shape in the deployed registry, plus the native-app form RFC 8252 s7.1
			// defines. A rule that rejected any of these would be too strict to ship.
			Entry("https", "https://app.example.com/callback"),
			Entry("https, no path", "https://app.example.com"),
			Entry("loopback http (RFC 8252 s7.3)", "http://localhost:5183/callback"),
			Entry("loopback by address", "http://127.0.0.1:8080/cb"),
			Entry("private-use scheme (RFC 8252 s7.1)", "com.espressif.rainmaker://callback"),
		)

		It("rejects an implicit or password grant for any client (negative)", func() {
			for _, g := range []string{"implicit", "password"} {
				in := publicClient("rm_" + g)
				in.GrantTypes = []string{g}
				_, err := svc.Create(in)
				Expect(err).To(HaveOccurred(), "grant %q must be rejected", g)
			}
		})

		It("rejects client_credentials for a public client, but allows it for a confidential one", func() {
			// The grant is client authentication, and a public client has no credentials to
			// present. It is a legitimate grant for a confidential client.
			pub := publicClient("rm_pub_cc")
			pub.GrantTypes = []string{"client_credentials"}
			_, err := svc.Create(pub)
			Expect(err).To(HaveOccurred())

			_, err = svc.Create(clients.CreateInput{
				ClientID: "rm_conf_cc", ClientName: "Server", ClientType: "confidential",
				GrantTypes: []string{"client_credentials"},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects an allowed_resource that is not an absolute URI without a fragment (negative)", func() {
			for _, bad := range []string{"/api", "api.example.com", "https://api.example.com/x#f"} {
				_, err := svc.Create(clients.CreateInput{
					ClientID: "rm_res", ClientName: "S", ClientType: "confidential",
					GrantTypes: []string{"client_credentials"}, AllowedResources: []string{bad},
				})
				Expect(err).To(HaveOccurred(), "RFC 8707 requires an absolute URI: %q", bad)
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

var _ = Describe("IsDangerousRedirectScheme", func() {
	DescribeTable("classifies a scheme case-insensitively",
		func(scheme string, dangerous bool) {
			Expect(clients.IsDangerousRedirectScheme(scheme)).To(Equal(dangerous))
		},
		Entry("javascript", "javascript", true),
		Entry("upper-case JavaScript", "JavaScript", true),
		Entry("data", "data", true),
		Entry("https", "https", false),
		Entry("a native app scheme", "com.example.app", false),
	)
})

var _ = Describe("clients registry — fields reserved for later steps", func() {
	var svc *clients.Service
	var db *oauth_clients_db.OAuthClientsDB

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		ctx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		svc = clients.NewService(ctx)
		db = oauth_clients_db.NewOAuthClientsDB(ctx)
	})

	// post_logout_redirect_uris is where /oauth2/logout may return a browser. It is a
	// registry field with a validator behind it, so the rules that keep redirect_uris from
	// becoming an open redirect have to hold here too.
	Describe("post_logout_redirect_uris", func() {
		withPostLogout := func(id string, uris []string) clients.CreateInput {
			in := publicNoProviders(id)
			in.PostLogoutRedirectURIs = uris
			return in
		}

		It("round-trips through create and read", func() {
			_, err := svc.Create(withPostLogout("rm_plo", []string{"https://app.example/bye"}))
			Expect(err).NotTo(HaveOccurred())
			got, err := svc.Get("rm_plo")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.PostLogoutRedirectURIs).To(Equal([]string{"https://app.example/bye"}))
			Expect(got.AllowsPostLogoutRedirectURI("https://app.example/bye")).To(BeTrue())
		})

		It("matches exactly — a near miss is not a match (negative)", func() {
			_, err := svc.Create(withPostLogout("rm_plo_exact", []string{"https://app.example/bye"}))
			Expect(err).NotTo(HaveOccurred())
			got, _ := svc.Get("rm_plo_exact")
			for _, near := range []string{
				"https://app.example/bye/",
				"https://app.example/bye?x=1",
				"https://app.example.evil.test/bye",
				"http://app.example/bye",
			} {
				Expect(got.AllowsPostLogoutRedirectURI(near)).To(BeFalse(), near)
			}
		})

		It("rejects a wildcard at write time, exactly like redirect_uris (negative)", func() {
			// An unusable value must not sit in the registry until the day someone signs out
			// through it. A wildcard here forwards a browser anywhere from the issuer's own
			// hostname, which is the most credible phishing origin this deployment owns.
			_, err := svc.Create(withPostLogout("rm_plo_star", []string{"https://*.example/bye"}))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("post_logout_redirect_uris"))
		})

		It("treats an empty list as none, never as any (negative)", func() {
			_, err := svc.Create(publicNoProviders("rm_plo_absent"))
			Expect(err).NotTo(HaveOccurred())
			got, _ := svc.Get("rm_plo_absent")
			Expect(got.AllowsPostLogoutRedirectURI("https://anywhere.example/")).To(BeFalse())
		})

		It("is replaced wholesale by an update, so a URL can be withdrawn", func() {
			_, err := svc.Create(withPostLogout("rm_plo_upd", []string{"https://old.example/bye"}))
			Expect(err).NotTo(HaveOccurred())
			_, err = svc.Update("rm_plo_upd", clients.UpdateInput{
				ClientName: "App", RedirectURIs: []string{"com.example://cb"},
				PostLogoutRedirectURIs: []string{"https://new.example/bye"},
				GrantTypes:             []string{"authorization_code"}, RequirePKCE: utils.Ptr(true),
			})
			Expect(err).NotTo(HaveOccurred())
			got, _ := svc.Get("rm_plo_upd")
			Expect(got.AllowsPostLogoutRedirectURI("https://old.example/bye")).To(BeFalse(),
				"a withdrawn URL must stop being a valid landing place")
			Expect(got.AllowsPostLogoutRedirectURI("https://new.example/bye")).To(BeTrue())
		})
	})

	Describe("allowed_providers", func() {
		It("round-trips through create and read", func() {
			_, err := svc.Create(clients.CreateInput{
				ClientID: "rm_prov", ClientName: "App", ClientType: "public",
				RedirectURIs: []string{"com.example://cb"}, GrantTypes: []string{"authorization_code"},
				AllowedProviders: []string{"rainmaker-public", "google"},
				RequirePKCE:      utils.Ptr(true),
			})
			Expect(err).NotTo(HaveOccurred())
			got, err := svc.Get("rm_prov")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.AllowedProviders).To(Equal([]string{"rainmaker-public", "google"}))
		})

		It("treats absent as no restriction, not as no providers", func() {
			// The opposite reading of allowed_resources, and deliberately so. A resource is
			// something a client must be entitled to; a provider is a menu. An empty menu
			// would lock out every client that predates the field.
			_, err := svc.Create(publicNoProviders("rm_prov_none"))
			Expect(err).NotTo(HaveOccurred())
			got, _ := svc.Get("rm_prov_none")
			Expect(got.AllowedProviders).To(BeEmpty())
			Expect(got.AllowsProvider("anything")).To(BeTrue(), "unset means every provider")
		})

		It("restricts to the listed providers once set (negative)", func() {
			in := publicNoProviders("rm_prov_one")
			in.AllowedProviders = []string{"rainmaker-public"}
			_, err := svc.Create(in)
			Expect(err).NotTo(HaveOccurred())
			got, _ := svc.Get("rm_prov_one")
			Expect(got.AllowsProvider("rainmaker-public")).To(BeTrue())
			Expect(got.AllowsProvider("google")).To(BeFalse())
		})

		It("rejects a blank provider name at write time (negative)", func() {
			in := publicNoProviders("rm_prov_blank")
			in.AllowedProviders = []string{"rainmaker-public", "  "}
			_, err := svc.Create(in)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("token_endpoint_auth_method", func() {
		It("takes a stored value over the one derived from client_type", func() {
			row := &oauth_clients_db.OAuthClientEntry{
				ClientID: "rm_auth_stored", ClientName: "Server", ClientType: "confidential",
				GrantTypes:              []string{"client_credentials"},
				TokenEndpointAuthMethod: "private_key_jwt",
			}
			Expect(db.CreateClient(row)).To(Succeed())
			got, err := svc.Get("rm_auth_stored")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.TokenEndpointAuthMethod).To(Equal("private_key_jwt"),
				"a stored value wins; the derived basic/none is only the default")
		})

		It("still derives the auth method when none is stored", func() {
			_, err := svc.Create(clients.CreateInput{
				ClientID: "rm_derived", ClientName: "Server", ClientType: "confidential",
				GrantTypes: []string{"client_credentials"},
			})
			Expect(err).NotTo(HaveOccurred())
			got, _ := svc.Get("rm_derived")
			Expect(got.TokenEndpointAuthMethod).To(Equal("client_secret_basic"))
		})
	})
})

func publicNoProviders(id string) clients.CreateInput {
	return clients.CreateInput{
		ClientID: id, ClientName: "App", ClientType: "public",
		RedirectURIs: []string{"com.example://cb"},
		GrantTypes:   []string{"authorization_code"},
		RequirePKCE:  utils.Ptr(true),
	}
}
