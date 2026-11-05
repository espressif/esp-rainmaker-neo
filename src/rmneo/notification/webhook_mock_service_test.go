// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	"context"
	"encoding/json"
	"io"
	"math"

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

var _ = Describe("WebhookMockService.SendTo", func() {
	const mockBase = "https://mock.example.com/prod"
	const userID = "webhook-mock-user"
	var ctx context.Context
	var mockHTTP *mock.MockHTTPClient

	BeforeEach(func() {
		test_utils.TestSetup()
		testmock.Reset()
		ctx = context.Background()
		mockHTTP = mock.NewMockHTTPClient()
		httpclient.Set(mockHTTP)

		rctx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
		Expect(user_integration_db.NewUserDB(rctx).RegisterClient(user_integration_db.UserIntegrationEntry{
			IntegrationID:    "webhook_mock",
			EndpointID:       "ep1",
			IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: "t", TokenType: "Bearer", ExpiresAt: math.MaxInt64},
		})).To(Succeed())
	})

	It("drops the notification where no mock is deployed", func() {
		Expect(NewWebhookMockService().SendTo(ctx, map[string]interface{}{"data": "x"}, []string{userID})).To(Succeed())
		Expect(mockHTTP.Requests).To(BeEmpty())
	})

	It("delivers to the mock's data endpoint with its API key", func() {
		Expect(ssmutil.StoreParameter(ctx, testmock.PARAM_NAME, `{"base_url":"`+mockBase+`","api_key":"k"}`)).To(Succeed())
		testmock.Reset()
		Expect(mockHTTP.RegisterResponse(mockBase+"/v1/data", "POST", 200, `{}`)).To(BeNil())

		Expect(NewWebhookMockService().SendTo(ctx, map[string]interface{}{"data": "x"}, []string{userID})).To(Succeed())

		Expect(mockHTTP.Requests).To(HaveLen(1))
		req := mockHTTP.Requests[0]
		Expect(req.URL.String()).To(Equal(mockBase + "/v1/data"))
		Expect(req.Header.Get("x-api-key")).To(Equal("k"))
		body, err := io.ReadAll(req.Body)
		Expect(err).NotTo(HaveOccurred())
		var sent map[string]interface{}
		Expect(json.Unmarshal(body, &sent)).To(Succeed())
		Expect(sent).To(Equal(map[string]interface{}{"data": "x", "uuid": userID + "#ep1"}))
	})
})
