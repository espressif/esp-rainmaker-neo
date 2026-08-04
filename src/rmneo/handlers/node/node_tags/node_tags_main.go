// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// UserTagsResponse represents the GET response with only user tags
type UserTagsResponse struct {
	User map[string]interface{} `json:"user"`
}

// UserTagsUpdateRequest represents the PUT request body
// User can only write user tags. Values can be strings or null (to delete).
type UserTagsUpdateRequest struct {
	User map[string]interface{} `json:"user"`
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	userCtx := user.NewContextWithAPIRequest(ctx, request)
	if userCtx == nil {
		rlog.Error(ctx).Msg("Failed to create user context")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	groupID := request.PathParameters["groupId"]
	nodeID := request.PathParameters["nodeId"]

	if groupID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("groupId is required")), nil
	}
	if nodeID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("nodeId is required")), nil
	}

	// Verify the caller has access to the group.
	groups, err := group.ListGroupForUser(userCtx, groupID, false)
	if err != nil {
		rlog.Error(userCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to verify group access")), nil
	}

	groupFound := false
	for _, g := range groups {
		if g.GroupID == groupID {
			groupFound = true
			break
		}
	}

	if !groupFound {
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("You don't have access to this group")), nil
	}

	// Reject a node that is not a member of this group (blocks cross-tenant access).
	// GetGroupNode also grants NodeAll on userCtx, authorizing the shadow op below.
	if _, err := group_node_db.NewGroupNodeDB(userCtx).GetGroupNode(groupID, nodeID); err != nil {
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("You don't have access to this node")), nil
	}

	switch request.HTTPMethod {
	case "GET":
		return handleGetUserTags(userCtx, nodeID)
	case "PUT":
		return handlePutUserTags(userCtx, nodeID, request.Body)
	default:
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}
}

func handleGetUserTags(rctx *rmngctx.RmngContext, nodeID string) (events.APIGatewayProxyResponse, error) {
	n := node.NewNode(nodeID)
	shadow, err := n.ReadFromIndexedReportedShadow(rctx)
	if err != nil {
		rlog.Error(rctx).Err(err).Msg("Failed to read indexed shadow")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to read tags")), nil
	}

	response := UserTagsResponse{
		User: make(map[string]interface{}),
	}

	if shadow.Data != nil && shadow.Data.User != nil && shadow.Data.User.Tags != nil {
		response.User = shadow.Data.User.Tags
	}

	return utils.APIGwRespJSON(http.StatusOK, response), nil
}

func handlePutUserTags(rctx *rmngctx.RmngContext, nodeID string, body string) (events.APIGatewayProxyResponse, error) {
	var req UserTagsUpdateRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	if len(req.User) == 0 {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("No tags provided")), nil
	}

	n := node.NewNode(nodeID)
	if err := n.UpdateTagsMap(rctx, req.User, node.TagTypeUser); err != nil {
		rlog.Error(rctx).Err(err).Msg("Failed to update user tags")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to update user tags")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("success")), nil
}

func main() {
	lambda.Start(handleRequest)
}
