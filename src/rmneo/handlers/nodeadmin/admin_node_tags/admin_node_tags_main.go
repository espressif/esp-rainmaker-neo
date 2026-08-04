// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// TagsResponse represents the GET response with all tag types
type TagsResponse struct {
	Admin  map[string]interface{} `json:"admin"`
	Device map[string]interface{} `json:"device"`
	User   map[string]interface{} `json:"user"`
}

// AdminTagsUpdateRequest represents the PUT request body
// Admin can write admin and user tags. Values can be strings or null (to delete).
type AdminTagsUpdateRequest struct {
	Admin map[string]interface{} `json:"admin,omitempty"`
	User  map[string]interface{} `json:"user,omitempty"`
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rctx := user.NewContextWithAPIRequest(ctx, request)
	if rctx == nil || rctx.GetAccessor() == nil || rctx.GetAccessor().GetID() == "" {
		rlog.Error(ctx).Msg("Failed to resolve user context for admin tags endpoint")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}
	userAccessor, ok := rctx.GetAccessor().(*user.User)
	if !ok {
		rlog.Error(rctx).Msg("Accessor is not a user; rejecting admin tags request")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}
	if !userAccessor.IsSuperAdmin(rctx) {
		rlog.Error(rctx).Msg("User is not authorized for admin tags endpoints")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	nodeID := request.PathParameters["nodeId"]
	if nodeID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("nodeId is required")), nil
	}

	// Use system context for shadow operations (admin already verified)
	systemActor := utils.NewSystemActor()
	systemContext := rmngctx.NewRmngContextWithCtx(ctx, systemActor)

	switch request.HTTPMethod {
	case "GET":
		return handleGetTags(systemContext, nodeID)
	case "PUT":
		return handlePutTags(systemContext, nodeID, request.Body)
	default:
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}
}

func handleGetTags(rctx *rmngctx.RmngContext, nodeID string) (events.APIGatewayProxyResponse, error) {
	n := node.NewNode(nodeID)
	shadow, err := n.ReadFromIndexedReportedShadow(rctx)
	if err != nil {
		rlog.Error(rctx).Err(err).Msg("Failed to read indexed shadow")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to read tags")), nil
	}

	response := TagsResponse{
		Admin:  make(map[string]interface{}),
		Device: make(map[string]interface{}),
		User:   make(map[string]interface{}),
	}

	if shadow.Data != nil {
		if shadow.Data.Admin != nil && shadow.Data.Admin.Tags != nil {
			response.Admin = shadow.Data.Admin.Tags
		}
		if shadow.Data.Device != nil && shadow.Data.Device.Tags != nil {
			response.Device = shadow.Data.Device.Tags
		}
		if shadow.Data.User != nil && shadow.Data.User.Tags != nil {
			response.User = shadow.Data.User.Tags
		}
	}

	return utils.APIGwRespJSON(http.StatusOK, response), nil
}

func handlePutTags(rctx *rmngctx.RmngContext, nodeID string, body string) (events.APIGatewayProxyResponse, error) {
	var req AdminTagsUpdateRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	if len(req.Admin) == 0 && len(req.User) == 0 {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("No tags provided")), nil
	}

	n := node.NewNode(nodeID)

	if len(req.Admin) > 0 {
		if err := n.UpdateTagsMap(rctx, req.Admin, node.TagTypeAdmin); err != nil {
			rlog.Error(rctx).Err(err).Msg("Failed to update admin tags")
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to update admin tags")), nil
		}
	}

	if len(req.User) > 0 {
		if err := n.UpdateTagsMap(rctx, req.User, node.TagTypeUser); err != nil {
			rlog.Error(rctx).Err(err).Msg("Failed to update user tags")
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to update user tags")), nil
		}
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("success")), nil
}

func main() {
	lambda.Start(handleRequest)
}
