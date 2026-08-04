// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

type NodeGroupsResponse struct {
	Group     string   `json:"group"`
	SubGroups []string `json:"sub_groups"`
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rctx := user.NewContextWithAPIRequest(ctx, request)
	if rctx == nil || rctx.GetAccessor() == nil || rctx.GetAccessor().GetID() == "" {
		rlog.Error(ctx).Msg("Failed to resolve user context for admin node-groups endpoint")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}
	userAccessor, ok := rctx.GetAccessor().(*user.User)
	if !ok {
		rlog.Error(rctx).Msg("Accessor is not a user; rejecting admin node-groups request")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}
	if !userAccessor.IsSuperAdmin(rctx) {
		rlog.Error(rctx).Msg("User is not authorized for admin node-groups endpoints")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	if request.HTTPMethod != "GET" {
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}

	nodeID := request.PathParameters["nodeId"]
	if nodeID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("nodeId is required")), nil
	}

	// Use system context to bypass normal permission checks
	systemActor := utils.NewSystemActor()
	systemContext := rmngctx.NewRmngContextWithCtx(ctx, systemActor)

	nodesGroups, err := group_node_db.NewGroupNodeDB(systemContext).GetNodesGroup(nodeID)
	if err != nil {
		rlog.Error(rctx).Err(err).Msg("Failed to get node's group")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to get node's group")), nil
	}

	response := NodeGroupsResponse{
		Group:     nodesGroups.Group,
		SubGroups: nodesGroups.SubGroups,
	}
	if response.SubGroups == nil {
		response.SubGroups = []string{}
	}

	return utils.APIGwRespJSON(http.StatusOK, response), nil
}

func main() {
	lambda.Start(handleRequest)
}
