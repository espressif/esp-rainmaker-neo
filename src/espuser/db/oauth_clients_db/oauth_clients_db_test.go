// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package oauth_clients_db_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOAuthClientsDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OAuthClientsDB Suite")
}

var _ = Describe("OAuthClientsDB", func() {
	var (
		dbMock *mock.DynamoDBMock
		db     *oauth_clients_db.OAuthClientsDB
	)

	BeforeEach(func() {
		dbMock = test_utils.SetupEspUserBackend(context.Background()).DBMock
		db = oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
	})

	newClient := func(id string) *oauth_clients_db.OAuthClientEntry {
		return &oauth_clients_db.OAuthClientEntry{
			ClientID:     id,
			ClientName:   "Test " + id,
			ClientType:   oauth_clients_db.ClientTypeConfidential,
			Secret:       "s3cret-" + id,
			RedirectURIs: []string{"https://app.example/cb"},
			GrantTypes:   []string{"authorization_code"},
			RequirePKCE:  utils.Ptr(false),
			CreatedAt:    100,
			UpdatedAt:    100,
		}
	}

	expectConditionFailed := func(err error, message string) {
		Expect(err).To(MatchError(message))
		var ccf *types.ConditionalCheckFailedException
		Expect(errors.As(err, &ccf)).To(BeTrue(), "error should wrap ConditionalCheckFailedException")
	}

	Describe("GetClient", func() {
		It("returns ErrOAuthClientNotFound for an unknown client", func() {
			entry, err := db.GetClient("rm_missing")
			Expect(err).To(BeIdenticalTo(oauth_clients_db.ErrOAuthClientNotFound))
			Expect(entry).To(BeNil())
		})

		It("round-trips every stored field, including a stored false RequirePKCE", func() {
			Expect(db.CreateClient(newClient("rm_a"))).To(Succeed())

			got, err := db.GetClient("rm_a")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ClientName).To(Equal("Test rm_a"))
			Expect(got.ClientType).To(Equal(oauth_clients_db.ClientTypeConfidential))
			Expect(got.Secret).To(Equal("s3cret-rm_a"))
			Expect(got.RedirectURIs).To(ConsistOf("https://app.example/cb"))
			Expect(got.GrantTypes).To(Equal([]string{"authorization_code"}))
			Expect(got.RequirePKCE).To(Equal(utils.Ptr(false)))
			Expect(got.CreatedAt).To(Equal(int64(100)))
		})
	})

	Describe("CreateClient", func() {
		It("fails on a duplicate client_id and leaves the existing row untouched", func() {
			Expect(db.CreateClient(newClient("rm_dup"))).To(Succeed())

			clash := newClient("rm_dup")
			clash.ClientName = "Clobber"
			expectConditionFailed(db.CreateClient(clash), "failed to create oauth client")

			got, err := db.GetClient("rm_dup")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ClientName).To(Equal("Test rm_dup"))
		})
	})

	Describe("UpdateClient", func() {
		It("sets the non-key fields of an existing client", func() {
			Expect(db.CreateClient(newClient("rm_upd"))).To(Succeed())

			changed := newClient("rm_upd")
			changed.ClientName = "Renamed"
			changed.UpdatedAt = 200
			Expect(db.UpdateClient(changed)).To(Succeed())

			got, err := db.GetClient("rm_upd")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ClientName).To(Equal("Renamed"))
			Expect(got.UpdatedAt).To(Equal(int64(200)))
			Expect(got.Secret).To(Equal("s3cret-rm_upd"))
		})

		It("fails on an unknown client and does not create it", func() {
			expectConditionFailed(db.UpdateClient(newClient("rm_ghost")), "failed to update oauth client")

			_, err := db.GetClient("rm_ghost")
			Expect(err).To(BeIdenticalTo(oauth_clients_db.ErrOAuthClientNotFound))
		})
	})

	Describe("AddRedirectURIs", func() {
		It("unions the URIs into the stored set and returns the merged set", func() {
			Expect(db.CreateClient(newClient("rm_add"))).To(Succeed())

			merged, err := db.AddRedirectURIs("rm_add", []string{"https://app.example/cb", "https://app.example/cb2"}, 300)
			Expect(err).NotTo(HaveOccurred())
			Expect(merged).To(ConsistOf("https://app.example/cb", "https://app.example/cb2"))

			got, err := db.GetClient("rm_add")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.RedirectURIs).To(ConsistOf("https://app.example/cb", "https://app.example/cb2"))
			Expect(got.UpdatedAt).To(Equal(int64(300)))
		})

		It("fails on an unknown client and does not create it", func() {
			merged, err := db.AddRedirectURIs("rm_ghost", []string{"https://app.example/cb"}, 300)
			expectConditionFailed(err, "failed to add redirect uris")
			Expect(merged).To(BeNil())

			_, err = db.GetClient("rm_ghost")
			Expect(err).To(BeIdenticalTo(oauth_clients_db.ErrOAuthClientNotFound))
		})
	})

	Describe("DeleteClient", func() {
		It("removes an existing client", func() {
			Expect(db.CreateClient(newClient("rm_del"))).To(Succeed())
			Expect(db.DeleteClient("rm_del")).To(Succeed())

			_, err := db.GetClient("rm_del")
			Expect(err).To(BeIdenticalTo(oauth_clients_db.ErrOAuthClientNotFound))
		})

		It("fails on an unknown client", func() {
			expectConditionFailed(db.DeleteClient("rm_ghost"), "failed to delete oauth client")
		})
	})

	Describe("ListClients", func() {
		It("returns nothing on an empty table", func() {
			clients, err := db.ListClients()
			Expect(err).NotTo(HaveOccurred())
			Expect(clients).To(BeEmpty())
		})

		It("returns every client across several scan pages", func() {
			dbMock.MaxPageItems = 2
			ids := make([]string, 0, 5)
			for i := 0; i < 5; i++ {
				id := fmt.Sprintf("rm_list_%d", i)
				ids = append(ids, id)
				Expect(db.CreateClient(newClient(id))).To(Succeed())
			}

			clients, err := db.ListClients()
			Expect(err).NotTo(HaveOccurred())
			got := make([]string, 0, len(clients))
			for _, c := range clients {
				got = append(got, c.ClientID)
			}
			Expect(got).To(ConsistOf(ids))
		})
	})
})
