// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package trigger

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// TriggerService implements the NodeService interface for trigger functionality
type TriggerService struct {
	service.BaseService
}

// NewTriggerService creates a new TriggerService
func NewTriggerService() *TriggerService {
	return &TriggerService{
		BaseService: service.BaseService{
			Name:      "trigger",
			Versioned: true,
		},
	}
}

// validateTriggerPayload ensures the payload has a 'triggers' array where each element has a unique ID
func (s *TriggerService) validateTriggerPayload(data interface{}) error {
	// Convert data to map
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return rmerror.NewRMError(nil, "trigger payload must be an object")
	}

	// Get triggers array
	triggersInterface, ok := dataMap["triggers"]
	if !ok {
		return rmerror.NewRMError(nil, "trigger payload must have 'triggers' field")
	}

	// Convert to array
	triggers, ok := triggersInterface.([]interface{})
	if !ok {
		return rmerror.NewRMError(nil, "'triggers' field must be an array")
	}

	// Track IDs to check for uniqueness
	seenIDs := make(map[string]bool)

	// Check each trigger element
	for i, trigger := range triggers {
		triggerMap, ok := trigger.(map[string]interface{})
		if !ok {
			return rmerror.NewRMError(nil, fmt.Sprintf("trigger element at index %d is not an object", i))
		}

		// Check for ID field
		id, ok := triggerMap["id"].(string)
		if !ok {
			return rmerror.NewRMError(nil, fmt.Sprintf("trigger element at index %d missing string 'id' field", i))
		}

		// Check for uniqueness
		if seenIDs[id] {
			return rmerror.NewRMError(nil, fmt.Sprintf("duplicate trigger id found: %s", id))
		}
		seenIDs[id] = true
	}

	return nil
}

// Get retrieves trigger data for a node
func (s *TriggerService) Get(rmngCtx *rmngctx.RmngContext, nodeID string) (interface{}, error) {
	// Check if user has permission to access this node
	if err := rmngCtx.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, rmerror.NewRMError(err, "unauthorized access to node trigger")
	}

	// Get trigger data from DB
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	triggerData, err := nodeDetailsDB.GetServiceData(nodeID, s.GetName())
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get trigger data")
	}

	// If trigger data doesn't exist, return empty object with empty triggers array
	if triggerData == nil {
		return map[string]interface{}{
			"triggers": []interface{}{},
		}, nil
	}

	return triggerData, nil
}

// Put updates trigger data for a node
func (s *TriggerService) Put(rmngCtx *rmngctx.RmngContext, nodeID string, data interface{}) error {
	// Check if user has permission to update this node
	if err := rmngCtx.IsAuthorized(utils.NodePutConfig, nodeID); err != nil {
		return rmerror.NewRMError(err, "unauthorized access to update node trigger")
	}

	// Validate trigger payload
	if err := s.validateTriggerPayload(data); err != nil {
		return err
	}

	// Update trigger data and version in a single DB write
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	err := nodeDetailsDB.UpdateServiceDataWithVersion(nodeID, s.GetName(), data)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update trigger data")
	}

	// Notify device about trigger update
	n := node.NewNode(nodeID)
	err = n.SendTriggerDetails(rmngCtx.Context)
	if err != nil {
		return rmerror.NewRMError(err, "failed to send trigger details to device")
	}

	return nil
}

// Delete removes trigger data for a node
func (s *TriggerService) Delete(rmngCtx *rmngctx.RmngContext, nodeID string) error {
	// Check if user has permission to update this node
	if err := rmngCtx.IsAuthorized(utils.NodeDeleteConfig, nodeID); err != nil {
		return rmerror.NewRMError(err, "unauthorized access to delete node trigger")
	}

	// Delete trigger data and update version in a single DB write
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	err := nodeDetailsDB.DeleteServiceDataWithVersion(nodeID, s.GetName())
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete trigger data")
	}

	// Notify device about trigger deletion
	n := node.NewNode(nodeID)
	err = n.SendTriggerDetails(rmngCtx.Context)
	if err != nil {
		return rmerror.NewRMError(err, "failed to send trigger details to device")
	}

	return nil
}

// Register registers the TriggerService with the global service registry
func Register() {
	service.Registry().RegisterNodeService(NewTriggerService())
}
