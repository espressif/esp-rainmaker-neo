// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// apiScheduleKey is the snake_case key clients use on the REST API.
// firmwareScheduleKey is the PascalCase key the device firmware reads on MQTT.
// The cloud translates between the two so the device-side wire shape stays
// untouched while the API uses idiomatic snake_case.
const (
	apiScheduleKey      = "schedules"
	firmwareScheduleKey = "Schedules"
)

// ScheduleService implements the NodeService interface for schedule functionality
type ScheduleService struct {
	service.BaseService
}

// NewScheduleService creates a new ScheduleService
func NewScheduleService() *ScheduleService {
	return &ScheduleService{
		BaseService: service.BaseService{
			Name:      "schedule",
			Versioned: true,
		},
	}
}

// Get retrieves schedule data for a node
func (s *ScheduleService) Get(rmngCtx *rmngctx.RmngContext, nodeID string) (interface{}, error) {
	// Check if user has permission to access this node
	if err := rmngCtx.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, rmerror.NewRMError(err, "unauthorized access to node schedule")
	}

	// Get schedule data from DB
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	scheduleData, err := nodeDetailsDB.GetServiceData(nodeID, s.GetName())
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get schedule data")
	}

	// If schedule data doesn't exist, return empty object
	if scheduleData == nil {
		return make(map[string]interface{}), nil
	}

	// Storage uses the firmware-side key ("Schedules") so the device wire
	// shape stays untouched. Translate to the API-side key ("schedules") on
	// the way out.
	if stored, ok := scheduleData.(map[string]interface{}); ok {
		if arr, exists := stored[firmwareScheduleKey]; exists {
			translated := make(map[string]interface{}, len(stored))
			for k, v := range stored {
				translated[k] = v
			}
			delete(translated, firmwareScheduleKey)
			translated[apiScheduleKey] = arr
			return translated, nil
		}
	}

	return scheduleData, nil
}

// Put updates schedule data for a node
func (s *ScheduleService) Put(rmngCtx *rmngctx.RmngContext, nodeID string, data interface{}) error {
	// Check if user has permission to update this node
	if err := rmngCtx.IsAuthorized(utils.NodePutConfig, nodeID); err != nil {
		return rmerror.NewRMError(err, "unauthorized access to update node schedule")
	}

	// API contract uses "schedules" (snake_case). Translate to the firmware
	// key ("Schedules") before storage so HandleGetSchedDetails forwards the
	// shape the device expects on MQTT.
	storeData := data
	if asMap, ok := data.(map[string]interface{}); ok {
		if arr, exists := asMap[apiScheduleKey]; exists {
			translated := make(map[string]interface{}, len(asMap))
			for k, v := range asMap {
				translated[k] = v
			}
			delete(translated, apiScheduleKey)
			translated[firmwareScheduleKey] = arr
			storeData = translated
		}
	}

	// Update schedule data and version in a single DB write
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	err := nodeDetailsDB.UpdateServiceDataWithVersion(nodeID, s.GetName(), storeData)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update schedule data")
	}

	// Notify device about schedule update
	n := node.NewNode(nodeID)
	err = n.SendScheduleDetails(rmngCtx.Context)
	if err != nil {
		return rmerror.NewRMError(err, "failed to send schedule details to device")
	}

	return nil
}

// Delete removes schedule data for a node
func (s *ScheduleService) Delete(rmngCtx *rmngctx.RmngContext, nodeID string) error {
	// Check if user has permission to update this node
	if err := rmngCtx.IsAuthorized(utils.NodeDeleteConfig, nodeID); err != nil {
		return rmerror.NewRMError(err, "unauthorized access to delete node schedule")
	}

	// Delete schedule data and update version in a single DB write
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	err := nodeDetailsDB.DeleteServiceDataWithVersion(nodeID, s.GetName())
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete schedule data")
	}

	// Notify device about schedule deletion
	n := node.NewNode(nodeID)
	err = n.SendScheduleDetails(rmngCtx.Context)
	if err != nil {
		return rmerror.NewRMError(err, "failed to send schedule details to device")
	}

	return nil
}

// Register registers the ScheduleService with the global service registry
func Register() {
	service.Registry().RegisterNodeService(NewScheduleService())
}
