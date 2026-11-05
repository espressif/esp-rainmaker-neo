// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"context"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/testmock"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGvaActionSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GVA Action Suite")
}

var _ = Describe("GVA mock routing", func() {
	const mockBase = "https://mock.example.com/prod"
	var ctx context.Context

	BeforeEach(func() {
		test_utils.TestSetup()
		testmock.Reset()
		ctx = context.Background()
	})

	It("sends an itest recipient to the mock's data endpoint for both calls", func() {
		Expect(ssmutil.StoreParameter(ctx, testmock.PARAM_NAME, `{"base_url":"`+mockBase+`","api_key":"k"}`)).To(Succeed())
		testmock.Reset()
		mockHTTP := mock.NewMockHTTPClient()
		httpclient.Set(mockHTTP)
		Expect(mockHTTP.RegisterResponse(mockBase+"/v1/gva/token", "POST", 200, `{"access_token":"mock-token"}`)).To(BeNil())

		route, err := resolveMockRoute(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(route).To(Equal(gvaRoute{reportURI: mockBase + "/v1/gva/data", syncURI: mockBase + "/v1/gva/data", token: "Bearer mock-token"}))
		Expect(mockHTTP.Requests[0].Header.Get("x-api-key")).To(Equal("k"))
	})

	It("never sends an itest recipient to HomeGraph when no mock is deployed", func() {
		mockHTTP := mock.NewMockHTTPClient()
		httpclient.Set(mockHTTP)

		_, err := resolveMockRoute(ctx)
		Expect(err).To(HaveOccurred())
		Expect(mockHTTP.Requests).To(BeEmpty())
	})

	Context("sending to itest-linked users", func() {
		userIDs := []string{"itest-user-1", "itest-user-2"}
		var mockHTTP *mock.MockHTTPClient

		BeforeEach(func() {
			Expect(ssmutil.StoreParameter(ctx, testmock.PARAM_NAME, `{"base_url":"`+mockBase+`","api_key":"k"}`)).To(Succeed())
			testmock.Reset()
			mockHTTP = mock.NewMockHTTPClient()
			httpclient.Set(mockHTTP)
			for _, userID := range userIDs {
				rctx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				Expect(user_integration_db.NewUserDB(rctx).RegisterClient(user_integration_db.UserIntegrationEntry{
					IntegrationID:    GVAPlatform,
					EndpointID:       gvaLinkEndpointID,
					IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: "t", TokenType: "Bearer"},
					TestMock:         true,
				})).To(Succeed())
			}
		})

		It("asks the mock for a token once per send, even when it fails", func() {
			Expect(mockHTTP.RegisterResponse(mockBase+"/v1/gva/token", "POST", 500, `{}`)).To(BeNil())

			Expect(NewGVANotification().SendTo(ctx, []GVARequestSyncRequest{{}}, userIDs)).To(Succeed())
			Expect(mockHTTP.Requests).To(HaveLen(1))
		})

		It("requests no token for a batch with nothing to report", func() {
			Expect(NewGVANotification().SendTo(ctx, GVANotificationBatch{}, userIDs)).To(Succeed())
			Expect(mockHTTP.Requests).To(BeEmpty())
		})
	})
})
