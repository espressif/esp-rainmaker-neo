// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smapi

import (
	"context"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/testmock"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSMAPIClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SMAPI Client Suite")
}

var _ = Describe("SMAPI client", func() {
	var httpM *mock.MockHTTPClient
	ctx := context.Background()

	BeforeEach(func() {
		test_utils.TestSetup()
		testmock.Reset()
		httpM = mock.NewMockHTTPClient()
		httpclient.Set(httpM)
	})

	Describe("GetAccessToken", func() {
		It("exchanges the refresh token for an access token", func() {
			Expect(httpM.RegisterResponse(LWATokenURL, "POST", 200, `{"access_token":"Atza|abc","token_type":"bearer","expires_in":3600}`)).To(Succeed())
			Expect(GetAccessToken(ctx, "cid", "secret", "refresh")).To(Equal("Atza|abc"))
		})

		// LWA's 4xx means the caller's own credentials were refused; only a 5xx or 429 is Amazon's fault.
		DescribeTable("classifies a refused exchange by who is at fault",
			func(status int, callerFault bool) {
				_ = httpM.RegisterResponse(LWATokenURL, "POST", status, `{"error":"x"}`)
				_, err := GetAccessToken(ctx, "id", "secret", "refresh")
				Expect(err).To(HaveOccurred())
				Expect(IsCallerError(err)).To(Equal(callerFault))
			},
			Entry("400 invalid_grant", 400, true),
			Entry("401 invalid_client", 401, true),
			Entry("429 rate limited", 429, false),
			Entry("500", 500, false),
			Entry("503", 503, false),
		)
	})

	Describe("ExchangeAuthorizationCode", func() {
		It("returns the refresh token minted for the code", func() {
			_ = httpM.RegisterResponse(LWATokenURL, "POST", 200, `{"access_token":"Atza|abc","refresh_token":"Atzr|xyz"}`)
			Expect(ExchangeAuthorizationCode(ctx, "cid", "secret", "code", "https://dash.example.com/alexa-lwa-callback")).To(Equal("Atzr|xyz"))
		})

		It("rejects a response without a refresh token", func() {
			_ = httpM.RegisterResponse(LWATokenURL, "POST", 200, `{"access_token":"Atza|abc"}`)
			_, err := ExchangeAuthorizationCode(ctx, "cid", "secret", "code", "https://dash.example.com/cb")
			Expect(err).To(HaveOccurred())
		})

		// An expired or reused code, or a redirect_uri other than the consent request's, is the caller's to fix.
		It("treats a refused code as the caller's fault", func() {
			_ = httpM.RegisterResponse(LWATokenURL, "POST", 400, `{"error":"invalid_grant"}`)
			_, err := ExchangeAuthorizationCode(ctx, "cid", "secret", "code", "https://dash.example.com/cb")
			Expect(IsCallerError(err)).To(BeTrue())
		})
	})

	Describe("GetVendorID", func() {
		BeforeEach(func() {
			_ = httpM.RegisterResponse(BaseURL+"/v1/vendors", "GET", 200, `{"vendors":[{"id":"M1ABC","name":"Acme"},{"id":"M2DEF","name":"Other"}]}`)
		})

		It("picks the profile's first vendor", func() {
			Expect(NewClient(ctx, "cid", "tok").GetVendorID(ctx)).To(Equal("M1ABC"))
		})

		It("treats a profile with no vendor as the caller's fault", func() {
			_ = httpM.RegisterResponse(BaseURL+"/v1/vendors", "GET", 200, `{"vendors":[]}`)
			_, err := NewClient(ctx, "cid", "tok").GetVendorID(ctx)
			Expect(IsCallerError(err)).To(BeTrue())
		})
	})

	Describe("CreateSkill", func() {
		It("returns the new skill id", func() {
			_ = httpM.RegisterResponse(BaseURL+"/v1/skills", "POST", 200, `{"skillId":"amzn1.ask.skill.xyz"}`)
			Expect(NewClient(ctx, "cid", "tok").CreateSkill(ctx, "M1ABC", MinimalSmartHomeManifest("ESP RainMaker", "https://x/privacy"))).To(Equal("amzn1.ask.skill.xyz"))
		})

		It("surfaces a rejected manifest as a caller error", func() {
			_ = httpM.RegisterResponse(BaseURL+"/v1/skills", "POST", 400, `{"message":"bad manifest"}`)
			_, err := NewClient(ctx, "cid", "tok").CreateSkill(ctx, "M1ABC", map[string]interface{}{})
			Expect(IsCallerError(err)).To(BeTrue())
		})
	})

	Describe("WaitForBuild", func() {
		It("returns once the build succeeds", func() {
			_ = httpM.RegisterResponse(BaseURL+"/v1/skills/s1/status", "GET", 200, `{"manifest":{"lastUpdateRequest":{"status":"SUCCEEDED"}}}`)
			Expect(NewClient(ctx, "cid", "tok").WaitForBuild(ctx, "s1", time.Second, time.Millisecond)).To(Succeed())
		})

		// The worker's context ends with the Lambda's deadline; waiting past it would only be cut off mid-sleep.
		It("stops waiting when the context is cancelled", func() {
			_ = httpM.RegisterResponse(BaseURL+"/v1/skills/s1/status", "GET", 200, `{"manifest":{"lastUpdateRequest":{"status":"IN_PROGRESS"}}}`)
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			err := NewClient(ctx, "cid", "tok").WaitForBuild(cancelled, "s1", time.Minute, time.Minute)
			Expect(err).To(MatchError(ContainSubstring("stopped waiting for skill build")))
		})

		It("reports SMAPI's reasons when the build fails", func() {
			_ = httpM.RegisterResponse(BaseURL+"/v1/skills/s1/status", "GET", 200,
				`{"manifest":{"lastUpdateRequest":{"status":"FAILED","errors":[{"code":"INVALID_ENDPOINT","message":"Endpoint is not invokable"},{"message":"Region EU missing"}]}}}`)
			err := NewClient(ctx, "cid", "tok").WaitForBuild(ctx, "s1", time.Second, time.Millisecond)
			Expect(err).To(MatchError("skill manifest build failed: Endpoint is not invokable; Region EU missing"))
		})
	})

	Describe("updates", func() {
		It("puts the manifest", func() {
			_ = httpM.RegisterResponse(BaseURL+"/v1/skills/s1/stages/"+Stage+"/manifest", "PUT", 202, "")
			Expect(NewClient(ctx, "cid", "tok").UpdateManifest(ctx, "s1", ManifestWithEvents("ESP RainMaker", "https://x/privacy"))).To(Succeed())
		})

		It("puts the account linking", func() {
			_ = httpM.RegisterResponse(BaseURL+"/v1/skills/s1/stages/"+Stage+"/accountLinkingClient", "PUT", 204, "")
			payload := AccountLinkingPayload("https://auth/authorize", "https://auth/token", "va-id", "va-secret")
			Expect(NewClient(ctx, "cid", "tok").UpdateAccountLinking(ctx, "s1", payload)).To(Succeed())
		})
	})

	Describe("with the itest webhook mock deployed", func() {
		const mockBase = "https://mock.example.com/prod"

		BeforeEach(func() {
			Expect(ssmutil.StoreParameter(ctx, testmock.PARAM_NAME, `{"base_url":"`+mockBase+`","api_key":"mock-key","lwa_client_id":"itest-cid"}`)).To(Succeed())
			testmock.Reset()
		})

		It("sends the itest LWA profile's LWA and SMAPI calls to the mock, with its API key", func() {
			_ = httpM.RegisterResponse(mockBase+"/v1/lwa/token", "POST", 200, `{"access_token":"Atza|mock"}`)
			_ = httpM.RegisterResponse(mockBase+"/v1/smapi/v1/vendors", "GET", 200, `{"vendors":[{"id":"MOCKVENDOR"}]}`)

			token, err := GetAccessToken(ctx, "itest-cid", "secret", "refresh")
			Expect(err).NotTo(HaveOccurred())
			Expect(NewClient(ctx, "itest-cid", token).GetVendorID(ctx)).To(Equal("MOCKVENDOR"))

			Expect(httpM.Requests).To(HaveLen(2))
			for _, r := range httpM.Requests {
				Expect(r.URL.String()).To(HavePrefix(mockBase + "/v1/"))
				Expect(r.Header.Get("x-api-key")).To(Equal("mock-key"))
			}
		})

		// A real admin on a shared test account must still create skills at Amazon.
		It("sends any other LWA profile to Amazon, without the mock's API key", func() {
			_ = httpM.RegisterResponse(LWATokenURL, "POST", 200, `{"access_token":"Atza|real"}`)
			_ = httpM.RegisterResponse(BaseURL+"/v1/vendors", "GET", 200, `{"vendors":[{"id":"M1ABC"}]}`)

			token, err := GetAccessToken(ctx, "real-cid", "secret", "refresh")
			Expect(err).NotTo(HaveOccurred())
			Expect(NewClient(ctx, "real-cid", token).GetVendorID(ctx)).To(Equal("M1ABC"))

			Expect(httpM.Requests).To(HaveLen(2))
			for _, r := range httpM.Requests {
				Expect(r.Header.Get("x-api-key")).To(BeEmpty())
			}
		})
	})

	Describe("GetSkillCredentials", func() {
		It("returns the skill's messaging credentials", func() {
			_ = httpM.RegisterResponse(BaseURL+"/v1/skills/s1/credentials", "GET", 200,
				`{"skillMessagingCredentials":{"clientId":"amzn1.application-oa2-client.acc","clientSecret":"shh"}}`)
			id, secret, err := NewClient(ctx, "cid", "tok").GetSkillCredentials(ctx, "s1")
			Expect(err).NotTo(HaveOccurred())
			Expect([]string{id, secret}).To(Equal([]string{"amzn1.application-oa2-client.acc", "shh"}))
		})

		// Alexa mints them only after a build grants the events permission; storing an empty pair would break AcceptGrant.
		DescribeTable("rejects a missing half",
			func(body string) {
				_ = httpM.RegisterResponse(BaseURL+"/v1/skills/s1/credentials", "GET", 200, body)
				_, _, err := NewClient(ctx, "cid", "tok").GetSkillCredentials(ctx, "s1")
				Expect(err).To(HaveOccurred())
			},
			Entry("no credentials", `{}`),
			Entry("no secret", `{"skillMessagingCredentials":{"clientId":"amzn1.app.acc"}}`),
			Entry("no client id", `{"skillMessagingCredentials":{"clientSecret":"shh"}}`),
		)
	})
})
