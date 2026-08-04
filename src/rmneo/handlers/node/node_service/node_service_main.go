// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/schedule"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/trigger"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// Initialize sets up the service registry with all available services
func initialize() {
	// Initialize service registry
	service.Initialize()

	// Register services
	schedule.Register()
	config.Register()
	timeseries.Register()
	trigger.Register()
}

// HandleRequest processes API Gateway requests for service operations
func HandleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Create the user context directly using the request
	userCtx := user.NewContextWithAPIRequest(ctx, request)

	// Add query parameters to the context for services that need them
	if len(request.QueryStringParameters) > 0 {
		userCtx.Context = context.WithValue(userCtx.Context, "query_params", request.QueryStringParameters)
	}

	// Get path parameters and other request details
	method := request.HTTPMethod
	pathParams := request.PathParameters
	groupID := pathParams["groupId"]
	nodeID := pathParams["nodeId"]

	var serviceName string

	switch request.Resource {
	// 1. Handle Timeseries (Must be explicit because of sub-paths /raw, /latest)
	case "/v1/groups/{groupId}/nodes/{nodeId}/timeseries/raw":
		serviceName = "timeseries"
		userCtx.Context = context.WithValue(userCtx.Context, "timeseries_type", "raw")
	case "/v1/groups/{groupId}/nodes/{nodeId}/timeseries/latest":
		serviceName = "timeseries"
		userCtx.Context = context.WithValue(userCtx.Context, "timeseries_type", "latest")
	case "/v1/groups/{groupId}/nodes/{nodeId}/timeseries/aggregates":
		serviceName = "timeseries"
		userCtx.Context = context.WithValue(userCtx.Context, "timeseries_type", "aggregates")

	case "/v1/groups/{groupId}/nodes/{nodeId}/{serviceName}":
		serviceName = request.PathParameters["serviceName"]
		// REST-plural URL segments map to internal (singular) service registry
		// names. Keeping the registry name preserves DB column names and the
		// shape that gets forwarded over MQTT to devices.
		switch serviceName {
		case "schedules":
			serviceName = "schedule"
		case "triggers":
			serviceName = "trigger"
		}

	default:
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Resource route not found")), nil
	}

	// Verify group access first - like in group_main.go
	groups, err := group.ListGroupForUser(userCtx, groupID, true)
	if err != nil {
		rlog.Error(userCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to verify group access")), nil
	}

	// Check if the user can access this specific group
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

	svc, err := service.Registry().GetService(serviceName)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus(err.Error())), nil
	}

	nodeSvc, ok := svc.(service.NodeService)
	if !ok {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Requested service is not a node service")), nil
	}

	// Handle the request based on the HTTP method
	switch method {
	case "GET":
		data, err := nodeSvc.Get(userCtx, nodeID)
		if err != nil {
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
		}
		return utils.APIGwRespJSON(http.StatusOK, data), nil

	case "PUT":
		var data interface{}
		if err := json.Unmarshal([]byte(request.Body), &data); err != nil {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request payload")), nil
		}

		if err := nodeSvc.Put(userCtx, nodeID, data); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, config.ErrNotPureMatter) {
				status = http.StatusForbidden
			}
			return utils.APIGwRespJSON(status, utils.NewAPIStatus(err.Error())), nil
		}
		return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("success")), nil

	case "DELETE":
		if err := nodeSvc.Delete(userCtx, nodeID); err != nil {
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
		}
		return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("success")), nil

	default:
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}
}

func main() {
	// Initialize the service registry
	initialize()

	// Start Lambda with the HandleRequest function
	lambda.Start(HandleRequest)
}
