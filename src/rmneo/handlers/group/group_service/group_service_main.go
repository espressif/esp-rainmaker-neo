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
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/automation"
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
	service.Registry().RegisterGroupService(automation.NewAutomationService())
}

// HandleRequest processes API Gateway requests for service operations
func HandleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Create the user context directly using the request
	userCtx := user.NewContextWithAPIRequest(ctx, request)

	// Get path parameters and other request details
	method := request.HTTPMethod
	pathParams := request.PathParameters
	groupID := pathParams["groupId"]
	serviceName := pathParams["serviceName"]

	// Check for additional path parameters like resourceID
	resourceID := pathParams["resourceId"]

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

	// Handle the request based on the HTTP method
	switch method {
	case "GET":
		// Get service from registry
		svc, err := service.Registry().GetService(serviceName)
		if err != nil {
			return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus(err.Error())), nil
		}

		// Check if it's a GroupService
		groupSvc, ok := svc.(service.GroupService)
		if !ok {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Requested service is not a group service")), nil
		}

		var data interface{}

		// Check if a resource ID is provided and if the service supports resource IDs
		if resourceID != "" && groupSvc.SupportsResourceID() {
			// Get specific resource data
			data, err = groupSvc.GetWithResourceID(userCtx, groupID, resourceID)
		} else {
			// Get all service data
			data, err = groupSvc.Get(userCtx, groupID)
		}

		if err != nil {
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
		}

		return utils.APIGwRespJSON(http.StatusOK, data), nil

	case "POST", "PUT":
		// POST creates a new resource on the collection (the service assigns
		// the ID); PUT updates the existing resource addressed by {resourceId}.

		// Parse request body into interface{} to handle both array and object payloads
		var data interface{}
		if err := json.Unmarshal([]byte(request.Body), &data); err != nil {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request payload")), nil
		}

		// Get service from registry
		svc, err := service.Registry().GetService(serviceName)
		if err != nil {
			return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus(err.Error())), nil
		}

		// Check if it's a GroupService
		groupSvc, ok := svc.(service.GroupService)
		if !ok {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Requested service is not a group service")), nil
		}

		var result interface{}
		switch {
		case method == "POST" && resourceID == "":
			// Create a new resource; the service generates its ID.
			result, err = groupSvc.Put(userCtx, groupID, data)
		case method == "PUT" && resourceID != "" && groupSvc.SupportsResourceID():
			// Update the resource addressed by its ID.
			result, err = groupSvc.PutWithResourceID(userCtx, groupID, resourceID, data)
		default:
			return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
		}

		if err != nil {
			// A malformed write (e.g. an automation action target outside the
			// group) is a client error, not a server fault.
			if errors.Is(err, automation.ErrActionTargetNotInGroup) ||
				errors.Is(err, automation.ErrConditionNodeNotInGroup) {
				return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(err.Error())), nil
			}
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
		}

		// If the service returned a result, use it, otherwise return a generic success message
		if result != nil {
			return utils.APIGwRespJSON(http.StatusOK, result), nil
		}
		return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("success")), nil

	case "DELETE":
		// Get service from registry
		svc, err := service.Registry().GetService(serviceName)
		if err != nil {
			return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus(err.Error())), nil
		}

		// Check if it's a GroupService
		groupSvc, ok := svc.(service.GroupService)
		if !ok {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Requested service is not a group service")), nil
		}

		// Check if a resource ID is provided and if the service supports resource IDs
		if resourceID != "" && groupSvc.SupportsResourceID() {
			// Delete specific resource
			if err := groupSvc.DeleteWithResourceID(userCtx, groupID, resourceID); err != nil {
				return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
			}
		} else {
			// Delete all service data
			if err := groupSvc.Delete(userCtx, groupID); err != nil {
				return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
			}
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
