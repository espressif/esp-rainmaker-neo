// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth_flows_db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAuthFlowsDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AuthFlowsDB Suite")
}

var _ = Describe("AuthFlowsDB", func() {
	var (
		backend *test_utils.EspUserBackend
		db      *auth_flows_db.AuthFlowsDB
		now     int64
	)

	flow := func(flowID string, expiresOn int64) *auth_flows_db.AuthFlow {
		return &auth_flows_db.AuthFlow{
			FlowID: flowID, ClientID: "client-1", RedirectURI: "https://app.example.com/cb",
			RequestedScope: []string{"openid", "profile"}, ExpiresOn: expiresOn,
		}
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		db = auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		now = time.Now().Unix()
	})

	Describe("CreateFlow and GetFlow", func() {
		It("round-trips a flow", func() {
			Expect(db.CreateFlow(flow("fl1", now+600))).To(Succeed())

			got, err := db.GetFlow("fl1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ClientID).To(Equal("client-1"))
			Expect(got.RequestedScope).To(Equal([]string{"openid", "profile"}))
		})

		It("refuses a second flow with the same id and keeps the first", func() {
			Expect(db.CreateFlow(flow("fl1", now+600))).To(Succeed())

			dup := flow("fl1", now+600)
			dup.ClientID = "client-2"
			err := db.CreateFlow(dup)
			Expect(err).To(MatchError("failed to put auth flow"))
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())

			got, err := db.GetFlow("fl1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ClientID).To(Equal("client-1"))
		})

		It("returns ErrFlowNotFound for an unknown flow", func() {
			_, err := db.GetFlow("missing")
			Expect(err).To(MatchError(auth_flows_db.ErrFlowNotFound))
		})

		It("returns ErrFlowExpired for a flow past expires_on", func() {
			Expect(db.CreateFlow(flow("fl1", now-1))).To(Succeed())

			_, err := db.GetFlow("fl1")
			Expect(err).To(MatchError(auth_flows_db.ErrFlowExpired))
		})

		It("treats expires_on 0 as no deadline", func() {
			Expect(db.CreateFlow(flow("fl1", 0))).To(Succeed())

			_, err := db.GetFlow("fl1")
			Expect(err).NotTo(HaveOccurred())
		})

		It("reports a failed read as an error, not as not-found", func() {
			boom := errors.New("throttled")
			backend.DBMock.NextGetItemError = boom

			_, err := db.GetFlow("fl1")
			Expect(err).To(MatchError("failed to get auth flow"))
			Expect(errors.Is(err, boom)).To(BeTrue())
		})
	})

	Describe("IssueCode", func() {
		It("binds the subject and code on an unbound flow and returns the whole flow", func() {
			Expect(db.CreateFlow(flow("fl1", now+600))).To(Succeed())

			got, err := db.IssueCode("fl1", "u1", []string{"openid"}, "code-1", "sid-1", 1234)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Subject).To(Equal("u1"))
			Expect(got.GrantedScope).To(Equal([]string{"openid"}))
			Expect(got.Code).To(Equal("code-1"))
			Expect(got.SID).To(Equal("sid-1"))
			Expect(got.AuthTime).To(Equal(int64(1234)))
			Expect(got.ClientID).To(Equal("client-1"), "the returned flow is the whole item, not only the updated attributes")
			Expect(got.RedirectURI).To(Equal("https://app.example.com/cb"))
		})

		It("returns ErrFlowNotFound and keeps the first subject on a bound flow", func() {
			Expect(db.CreateFlow(flow("fl1", now+600))).To(Succeed())
			_, err := db.IssueCode("fl1", "u1", []string{"openid"}, "code-1", "", 0)
			Expect(err).NotTo(HaveOccurred())

			_, err = db.IssueCode("fl1", "u2", []string{"openid"}, "code-2", "", 0)
			Expect(err).To(MatchError(auth_flows_db.ErrFlowNotFound))

			got, err := db.GetFlow("fl1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Subject).To(Equal("u1"))
			Expect(got.Code).To(Equal("code-1"))
		})

		It("returns ErrFlowNotFound on an expired flow", func() {
			Expect(db.CreateFlow(flow("fl1", now-1))).To(Succeed())

			_, err := db.IssueCode("fl1", "u1", []string{"openid"}, "code-1", "", 0)
			Expect(err).To(MatchError(auth_flows_db.ErrFlowNotFound))
		})

		It("returns ErrFlowNotFound and creates nothing for an unknown flow", func() {
			_, err := db.IssueCode("missing", "u1", []string{"openid"}, "code-1", "", 0)
			Expect(err).To(MatchError(auth_flows_db.ErrFlowNotFound))

			_, err = db.GetFlow("missing")
			Expect(err).To(MatchError(auth_flows_db.ErrFlowNotFound))
		})
	})

	Describe("SetUpstreamLeg", func() {
		It("stores the upstream leg on an unbound flow", func() {
			Expect(db.CreateFlow(flow("fl1", now+600))).To(Succeed())

			Expect(db.SetUpstreamLeg("fl1", "google", "st", "nonce", "verifier")).To(Succeed())

			got, err := db.GetFlow("fl1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Provider).To(Equal("google"))
			Expect(got.UpstreamState).To(Equal("st"))
			Expect(got.UpstreamNonce).To(Equal("nonce"))
			Expect(got.UpstreamPKCEVerifier).To(Equal("verifier"))
		})

		It("fails on a flow whose subject is already bound", func() {
			Expect(db.CreateFlow(flow("fl1", now+600))).To(Succeed())
			_, err := db.IssueCode("fl1", "u1", []string{"openid"}, "code-1", "", 0)
			Expect(err).NotTo(HaveOccurred())

			err = db.SetUpstreamLeg("fl1", "google", "st", "nonce", "verifier")
			Expect(err).To(MatchError("failed to set upstream federation leg"))
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())
		})

		It("fails and creates nothing for an unknown flow", func() {
			err := db.SetUpstreamLeg("missing", "google", "st", "nonce", "verifier")
			Expect(err).To(MatchError("failed to set upstream federation leg"))
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())

			_, err = db.GetFlow("missing")
			Expect(err).To(MatchError(auth_flows_db.ErrFlowNotFound))
		})
	})

	Describe("GetFlowByCode", func() {
		It("resolves an issued code to its flow", func() {
			Expect(db.CreateFlow(flow("fl1", now+600))).To(Succeed())
			_, err := db.IssueCode("fl1", "u1", []string{"openid"}, "code-1", "", 0)
			Expect(err).NotTo(HaveOccurred())

			got, err := db.GetFlowByCode("code-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.FlowID).To(Equal("fl1"))
			Expect(got.Subject).To(Equal("u1"))
			Expect(got.ClientID).To(Equal("client-1"))
		})

		It("returns ErrFlowNotFound for an unknown code", func() {
			_, err := db.GetFlowByCode("nope")
			Expect(err).To(MatchError(auth_flows_db.ErrFlowNotFound))
		})

		It("returns ErrFlowExpired for a code whose flow is past expires_on", func() {
			expired := flow("fl1", now-1)
			expired.Code = "code-1"
			Expect(db.CreateFlow(expired)).To(Succeed())

			_, err := db.GetFlowByCode("code-1")
			Expect(err).To(MatchError(auth_flows_db.ErrFlowExpired))
		})
	})

	Describe("ConsumeCode", func() {
		It("deletes the flow once and refuses a replay", func() {
			Expect(db.CreateFlow(flow("fl1", now+600))).To(Succeed())

			Expect(db.ConsumeCode("fl1")).To(Succeed())

			err := db.ConsumeCode("fl1")
			Expect(err).To(MatchError("failed to consume authorization code"))
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())
		})
	})
})
