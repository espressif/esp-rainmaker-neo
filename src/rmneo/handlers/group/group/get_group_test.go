// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("handleGetGroup", func() {
	const (
		userID      = "get-group-user-id"
		otherUserID = "get-group-other-user-id"
		nodeID      = "get-group-node-1"
		controller  = "get-group-controller-1"
	)

	var (
		ctx     context.Context
		groupID string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		test_utils.SetupTestUser(ctx, userID, "get-group-user@example.com")
		test_utils.SetupTestUser(ctx, otherUserID, "get-group-other@example.com")

		groupID = CreateGroup(ctx, "Get Group Test", userID)
		test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)
		// The controller is a node of the group like any other; that membership is what scopes it.
		test_utils.ManuallyAddNodeToGroup(ctx, groupID, controller)
	})

	asUser := func(id, gid string) events.APIGatewayProxyRequest {
		return events.APIGatewayProxyRequest{
			HTTPMethod:     "GET",
			Resource:       "/v1/groups/{groupId}",
			PathParameters: map[string]string{"groupId": gid},
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             id,
					CognitoAuthenticationProvider: "https://issuer.example:" + id,
				},
			},
		}
	}

	asController := func(identity events.APIGatewayRequestIdentity, gid string) events.APIGatewayProxyRequest {
		return events.APIGatewayProxyRequest{
			HTTPMethod:     "GET",
			Resource:       "/v1/groups/{groupId}",
			PathParameters: map[string]string{"groupId": gid},
			RequestContext: events.APIGatewayProxyRequestContext{Identity: identity},
		}
	}

	unknownDevice := test_utils.CredentialProviderIdentity(strings.Repeat("cd", 32))

	decode := func(response events.APIGatewayProxyResponse) map[string]interface{} {
		var body map[string]interface{}
		Expect(json.Unmarshal([]byte(response.Body), &body)).To(Succeed())
		return body
	}

	Describe("as a member user", func() {
		It("returns the group with its node ids and primary access", func() {
			response, err := handleGetGroup(ctx, asUser(userID, groupID))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			body := decode(response)
			Expect(body["group_id"]).To(Equal(groupID))
			Expect(body["group_name"]).To(Equal("Get Group Test"))
			Expect(body["access_type"]).To(Equal(string(utils.GroupPrimaryAccess)))
			Expect(body["node_ids"]).To(ConsistOf(nodeID, controller))
		})
	})

	Describe("as a non-member user", func() {
		It("is refused without revealing whether the group exists", func() {
			response, err := handleGetGroup(ctx, asUser(otherUserID, groupID))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("returns the same status for a group id that does not exist", func() {
			response, err := handleGetGroup(ctx, asUser(userID, "no-such-group"))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("as a controller", func() {
		It("reads the group its node belongs to, despite holding no user membership row", func() {
			response, err := handleGetGroup(ctx, asController(test_utils.DeviceIdentity(controller), groupID))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			body := decode(response)
			Expect(body["group_id"]).To(Equal(groupID))
			Expect(body["node_ids"]).To(ConsistOf(nodeID, controller))
			Expect(body["access_type"]).To(Equal(string(utils.GroupDeviceAccess)))
		})

		It("is refused on a group its node does not belong to", func() {
			otherGroupID := CreateGroup(ctx, "Someone Else", otherUserID)

			response, err := handleGetGroup(ctx, asController(test_utils.DeviceIdentity(controller), otherGroupID))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("is unauthorized when its certificate is attached to no node", func() {
			response, err := handleGetGroup(ctx, asController(unknownDevice, groupID))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		DescribeTable("answers 401 rather than panicking on every GET route the controller role reaches",
			func(resource string, params map[string]string) {
				request := asController(unknownDevice, groupID)
				request.Resource = resource
				request.PathParameters = params
				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
			},
			Entry("group users", "/v1/groups/{groupId}/users", map[string]string{"groupId": "g"}),
			Entry("subgroup users", "/v1/groups/{groupId}/subgroups/{subGroupId}/users", map[string]string{"groupId": "g", "subGroupId": "s"}),
			Entry("group sharing requests", "/v1/groups/{groupId}/sharing-requests", map[string]string{"groupId": "g"}),
			Entry("subgroup sharing requests", "/v1/groups/{groupId}/subgroups/{subGroupId}/sharing-requests", map[string]string{"groupId": "g", "subGroupId": "s"}),
		)
	})

	Describe("request validation", func() {
		It("rejects a missing group id", func() {
			response, err := handleGetGroup(ctx, asUser(userID, ""))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("routing", func() {
		It("dispatches GET /v1/groups/{groupId} to handleGetGroup", func() {
			response, err := handleRequest(ctx, asUser(userID, groupID))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(decode(response)["group_id"]).To(Equal(groupID))
		})
	})
})
