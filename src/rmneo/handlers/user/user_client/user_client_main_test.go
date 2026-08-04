// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Notification Endpoints", func() {
	var (
		ctx    context.Context
		userID string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		userID = "test-user-id"

		_, _ = test_utils.SetupTestUser(ctx, userID, "test-user@example.com")
	})

	// End users federate through the identity pool's OIDC provider, so the provider string is "<issuer>:<sub>" with no "CognitoSignIn:" segment — extractCallerIdentity reads it as an OIDC caller and resolves the sub via ResolveESPUserByID.
	identityFor := func(uid string) events.APIGatewayProxyRequestContext {
		return events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoAuthenticationProvider: "https://issuer.example:" + uid,
			},
		}
	}

	Describe("PUT /v1/integrations/{integrationId}/endpoints", func() {
		It("registers an OAuth-style endpoint (alexa)", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "PUT",
				PathParameters: map[string]string{"integrationId": "alexa"},
				Body:           `{"delivery_credentials": {"access_token": "at", "refresh_token": "rt", "expires_at": 1779852720}}`,
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp EndpointStatusResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.EndpointID).To(Equal(user_integration_db.EncodeEndpointID("alexa")))

			item := test_utils.QuickGetItem(user_integration_db.UserEndpointsTable, map[string]types.AttributeValue{
				"user_id":              &types.AttributeValueMemberS{Value: userID},
				"integration_endpoint": &types.AttributeValueMemberS{Value: "alexa#" + user_integration_db.EncodeEndpointID("alexa")},
			})
			Expect(item).To(Not(BeNil()))
			tok := item["integration_token"].(*types.AttributeValueMemberM).Value
			Expect(tok["access_token"].(*types.AttributeValueMemberS).Value).To(Equal("at"))
			Expect(tok["refresh_token"].(*types.AttributeValueMemberS).Value).To(Equal("rt"))
			Expect(tok["access_expires_at"].(*types.AttributeValueMemberN).Value).To(Equal("1779852720"))
		})

		It("is idempotent — replaces existing credentials on repeated PUT", func() {
			base := events.APIGatewayProxyRequest{
				HTTPMethod:     "PUT",
				PathParameters: map[string]string{"integrationId": "alexa"},
				RequestContext: identityFor(userID),
			}

			first := base
			first.Body = `{"delivery_credentials": {"access_token": "first", "refresh_token": "rt"}}`
			response, err := handleRequest(ctx, first)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			second := base
			second.Body = `{"delivery_credentials": {"access_token": "second", "refresh_token": "rt"}}`
			response, err = handleRequest(ctx, second)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			item := test_utils.QuickGetItem(user_integration_db.UserEndpointsTable, map[string]types.AttributeValue{
				"user_id":              &types.AttributeValueMemberS{Value: userID},
				"integration_endpoint": &types.AttributeValueMemberS{Value: "alexa#" + user_integration_db.EncodeEndpointID("alexa")},
			})
			Expect(item).To(Not(BeNil()))
			tok := item["integration_token"].(*types.AttributeValueMemberM).Value
			Expect(tok["access_token"].(*types.AttributeValueMemberS).Value).To(Equal("second"))
		})

		It("registers an APNS endpoint via SNS CreatePlatformEndpoint", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "PUT",
				PathParameters: map[string]string{"integrationId": "apns_sandbox_RainMaker"},
				Body:           `{"delivery_credentials": {"app_token": "device-token-hex"}, "locale": "en_US"}`,
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp EndpointStatusResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			expectedArn := "arn:aws:sns:us-east-1:123456789012:app/APNS_SANDBOX/RainMaker/mock-endpoint-id"
			Expect(resp.EndpointID).To(Equal(user_integration_db.EncodeEndpointID(expectedArn)))

			snsMock := awscommon.GetSNSClient().(*mock.SNSMock)
			endpointCalls := snsMock.GetCreatePlatformEndpointCalls()
			Expect(endpointCalls).To(HaveLen(1))
			Expect(*endpointCalls[0].PlatformApplicationArn).To(Equal("arn:aws:sns:us-east-1:123456789012:app/APNS_SANDBOX/RainMaker"))
			Expect(*endpointCalls[0].Token).To(Equal("device-token-hex"))

			item := test_utils.QuickGetItem(user_integration_db.UserEndpointsTable, map[string]types.AttributeValue{
				"user_id":              &types.AttributeValueMemberS{Value: userID},
				"integration_endpoint": &types.AttributeValueMemberS{Value: "APNS_SANDBOX_RainMaker#" + user_integration_db.EncodeEndpointID(expectedArn)},
			})
			Expect(item).To(Not(BeNil()))
			Expect(item["sns_endpoint_arn"].(*types.AttributeValueMemberS).Value).To(Equal(expectedArn))
		})

		It("registers an GCM endpoint via SNS CreatePlatformEndpoint", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "PUT",
				PathParameters: map[string]string{"integrationId": "gcm_RainMaker"},
				Body:           `{"delivery_credentials": {"app_token": "firebase-token"}, "locale": "es_ES"}`,
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			snsMock := awscommon.GetSNSClient().(*mock.SNSMock)
			endpointCalls := snsMock.GetCreatePlatformEndpointCalls()
			Expect(endpointCalls).To(HaveLen(1))
			Expect(*endpointCalls[0].PlatformApplicationArn).To(Equal("arn:aws:sns:us-east-1:123456789012:app/GCM/RainMaker"))
			Expect(*endpointCalls[0].Token).To(Equal("firebase-token"))

			expectedArn := "arn:aws:sns:us-east-1:123456789012:app/GCM/RainMaker/mock-endpoint-id"
			item := test_utils.QuickGetItem(user_integration_db.UserEndpointsTable, map[string]types.AttributeValue{
				"user_id":              &types.AttributeValueMemberS{Value: userID},
				"integration_endpoint": &types.AttributeValueMemberS{Value: "GCM_RainMaker#" + user_integration_db.EncodeEndpointID(expectedArn)},
			})
			Expect(item).To(Not(BeNil()))
			Expect(item["sns_endpoint_arn"].(*types.AttributeValueMemberS).Value).To(Equal(expectedArn))
		})

		It("propagates SNS errors during endpoint creation", func() {
			snsMock := awscommon.GetSNSClient().(*mock.SNSMock)
			snsMock.CreatePlatformEndpointError = rmerror.NewRMError(nil, "simulated SNS error")

			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "PUT",
				PathParameters: map[string]string{"integrationId": "apns_sandbox_RainMaker"},
				Body:           `{"delivery_credentials": {"app_token": "device-token"}}`,
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to create platform endpoint"))

			snsMock.CreatePlatformEndpointError = nil
		})

		It("rejects missing app_token for push integrations", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "PUT",
				PathParameters: map[string]string{"integrationId": "apns_sandbox_RainMaker"},
				Body:           `{"delivery_credentials": {}}`,
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Missing delivery_credentials.app_token for push integration"))
		})

		It("rejects missing delivery_credentials for OAuth integrations", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "PUT",
				PathParameters: map[string]string{"integrationId": "alexa"},
				Body:           `{"delivery_credentials": {}}`,
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Missing delivery_credentials"))
		})

		It("rejects an invalid JSON body", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "PUT",
				PathParameters: map[string]string{"integrationId": "alexa"},
				Body:           "invalid-json",
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Invalid request body"))
		})

		It("rejects missing integrationId", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "PUT",
				Body:           `{"delivery_credentials": {"app_token": "x"}}`,
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Missing integrationId"))
		})
	})

	Describe("DELETE /v1/integrations/{integrationId}/endpoints/{endpointId}", func() {
		// registerAndGetEndpointID PUTs the given integration and returns the endpoint_id the response handed back, so the matching DELETE can address it.
		registerAndGetEndpointID := func(integrationID, body string) string {
			registerReq := events.APIGatewayProxyRequest{
				HTTPMethod:     "PUT",
				PathParameters: map[string]string{"integrationId": integrationID},
				Body:           body,
				RequestContext: identityFor(userID),
			}
			response, err := handleRequest(ctx, registerReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			var resp EndpointStatusResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.EndpointID).ToNot(BeEmpty())
			return resp.EndpointID
		}

		It("unregisters an OAuth-style endpoint", func() {
			endpointID := registerAndGetEndpointID("alexa", `{"delivery_credentials": {"access_token": "at", "refresh_token": "rt", "expires_at": 1779852720}}`)

			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "DELETE",
				PathParameters: map[string]string{"integrationId": "alexa", "endpointId": endpointID},
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("unregisters a push endpoint and deletes the SNS endpoint", func() {
			endpointID := registerAndGetEndpointID("apns_sandbox_RainMaker", `{"delivery_credentials": {"app_token": "device-token"}}`)

			Expect(test_utils.QuickGetItem(user_integration_db.UserEndpointsTable, map[string]types.AttributeValue{
				"user_id":              &types.AttributeValueMemberS{Value: userID},
				"integration_endpoint": &types.AttributeValueMemberS{Value: "APNS_SANDBOX_RainMaker#" + endpointID},
			})).To(Not(BeNil()))

			unregisterReq := events.APIGatewayProxyRequest{
				HTTPMethod:     "DELETE",
				PathParameters: map[string]string{"integrationId": "apns_sandbox_RainMaker", "endpointId": endpointID},
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, unregisterReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			snsMock := awscommon.GetSNSClient().(*mock.SNSMock)
			deleteCalls := snsMock.GetDeleteEndpointCalls()
			Expect(deleteCalls).To(HaveLen(1))
			// endpointID is the base64url-encoded id; DeleteEndpoint is called with the raw SNS ARN (decoded form).
			Expect(*deleteCalls[0].EndpointArn).To(Equal(user_integration_db.DecodeEndpointID(endpointID)))

			Expect(test_utils.QuickGetItem(user_integration_db.UserEndpointsTable, map[string]types.AttributeValue{
				"user_id":              &types.AttributeValueMemberS{Value: userID},
				"integration_endpoint": &types.AttributeValueMemberS{Value: "APNS_SANDBOX_RainMaker#" + endpointID},
			})).To(BeNil())
		})

		It("propagates SNS errors during endpoint deletion", func() {
			endpointID := registerAndGetEndpointID("apns_sandbox_RainMaker", `{"delivery_credentials": {"app_token": "device-token"}}`)

			snsMock := awscommon.GetSNSClient().(*mock.SNSMock)
			snsMock.DeleteEndpointError = rmerror.NewRMError(nil, "simulated delete error")

			unregisterReq := events.APIGatewayProxyRequest{
				HTTPMethod:     "DELETE",
				PathParameters: map[string]string{"integrationId": "apns_sandbox_RainMaker", "endpointId": endpointID},
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, unregisterReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))

			snsMock.DeleteEndpointError = nil
		})

		It("succeeds when a push endpoint row is already gone", func() {
			endpointID := user_integration_db.EncodeEndpointID("arn:aws:sns:eu-west-1:000000000000:endpoint/GCM/absent/" + userID)

			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "DELETE",
				PathParameters: map[string]string{"integrationId": "gcm_absent", "endpointId": endpointID},
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).ToNot(ContainSubstring("Failed to retrieve endpoint"))

			// No SNS endpoint existed, so nothing may be deleted from SNS.
			Expect(awscommon.GetSNSClient().(*mock.SNSMock).GetDeleteEndpointCalls()).To(BeEmpty())
		})

		It("rejects missing integrationId", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "DELETE",
				PathParameters: map[string]string{"endpointId": "any"},
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Missing integrationId"))
		})

		It("rejects missing endpointId", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "DELETE",
				PathParameters: map[string]string{"integrationId": "alexa"},
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Missing endpointId"))
		})
	})

	Describe("HTTP methods", func() {
		It("rejects POST (use PUT)", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod:     "POST",
				PathParameters: map[string]string{"integrationId": "alexa"},
				RequestContext: identityFor(userID),
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))
			Expect(response.Body).To(ContainSubstring("Method not allowed"))
		})
	})
})

func TestRegisterClientMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Notification Endpoints Suite")
}
