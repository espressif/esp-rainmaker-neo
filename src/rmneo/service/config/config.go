// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/timeseries_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/collections"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"reflect"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

var ErrNotPureMatter = errors.New("config write is only allowed for pure Matter nodes")

// NodeCfg represents the configuration structure for a node.
type NodeCfg struct {
	DataModel string                    `json:"data_model,omitempty"` //"default" (RMNG) or "matter"
	Devices   []NodeCfgDevice           `json:"devices,omitempty"`
	Endpoints map[string]MatterEndpoint `json:"endpoints,omitempty"`
	Services  []NodeCfgService          `json:"services,omitempty"`
	Info      NodeCfgInfo               `json:"info"`
}
type NodeCfgDevice struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`              // example: esp.device.switch, esp.device.lightbulb, esp.device.other
	Primary    string               `json:"primary,omitempty"` // ID of the primary parameter
	Params     []NodeCfgDeviceParam `json:"params"`
	Attributes []NodeCfgDeviceAttr  `json:"attributes,omitempty"`
}

type NodeCfgDeviceParam struct {
	ID         string              `json:"id"`
	Type       string              `json:"type"`                 // example: esp.param.name, esp.param.power
	DataType   string              `json:"data_type"`            // bool, int, float, string, array
	Properties []string            `json:"properties,omitempty"` // read, write, persist, time_series, indexed
	Bounds     *NodeCfgParamBounds `json:"bounds,omitempty"`
	UIType     string              `json:"ui_type,omitempty"` // example: esp.ui.slider, esp.ui.toggle, esp.ui.hidden
}

type NodeCfgParamBounds struct {
	Min  *int `json:"min,omitempty"`
	Max  *int `json:"max,omitempty"`
	Step *int `json:"step,omitempty"`
}

type NodeCfgDeviceAttr struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

type NodeCfgService struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`              // esp.service.time, esp.service.system
	Primary    string               `json:"primary,omitempty"` // ID of the primary parameter
	Params     []NodeCfgDeviceParam `json:"params,omitempty"`  // esp.param.tz, esp.param.tz_posix, esp.param.reboot, esp.param.factory-reset, esp.param.wifi-reset
	Attributes []NodeCfgDeviceAttr  `json:"attributes,omitempty"`
}

type NodeCfgInfo struct {
	Type      string `json:"type,omitempty"`
	FWVersion string `json:"fw_version"`
	Model     string `json:"model,omitempty"`
	Name      string `json:"name,omitempty"`
	// Manufacturer is the brand the device reports for itself. Optional, and expected to be
	// absent on most firmware; when present it overrides the deployment's configured brand in
	// voice-assistant integrations, so one image can ship under several brands.
	Manufacturer string `json:"manufacturer,omitempty"`
}

type MatterEndpoint struct {
	DeviceType string                 `json:"dt,omitempty"`
	Clusters   MatterEndpointClusters `json:"c"`
}

type MatterEndpointClusters struct {
	Servers map[string]MatterServerCluster `json:"s,omitempty"`
	Clients map[string]MatterClientCluster `json:"c,omitempty"`
}

// MatterServerCluster represents a matter server cluster
type MatterServerCluster struct {
	Attributes []string `json:"a,omitempty"`
	Events     []string `json:"e,omitempty"`
	Commands   []string `json:"c,omitempty"` // accepted commands
	Indexed    []string `json:"i,omitempty"`
	TimeSeries []string `json:"ts,omitempty"`
	// ConfigOnly holds config-only attribute values, keyed by attribute ID.
	// These appear in node config but never in shadow params/state updates.
	ConfigOnly map[string]interface{} `json:"v,omitempty"`
}

// MatterClientCluster represents a matter client cluster
type MatterClientCluster struct {
	Commands []string `json:"c,omitempty"` // generated commands
}

// ToNodeCfg converts a map or any compatible type to a NodeCfg struct
func ToNodeCfg(source any) NodeCfg {
	var nodeCfg NodeCfg
	utils.ConvertAnyToAny(source, &nodeCfg)
	return nodeCfg
}

// ToMap converts NodeCfg struct to map[string]interface{} for database storage
func (nc NodeCfg) ToMap() map[string]interface{} {
	var result map[string]interface{}
	utils.ConvertAnyToAny(nc, &result)
	return result
}

// ConfigService implements the NodeService interface for config functionality
type ConfigService struct {
	service.BaseService
}

// NewConfigService creates a new ConfigService
func NewConfigService() *ConfigService {
	return &ConfigService{
		BaseService: service.BaseService{
			Name: "config",
		},
	}
}

// Get retrieves the entire config data for a node
func (s *ConfigService) Get(rmngCtx *rmngctx.RmngContext, nodeID string) (interface{}, error) {
	// Check if user has permission to access this node
	if err := rmngCtx.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, rmerror.NewRMError(err, "unauthorized access to node config")
	}

	// Get node details from DB
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	nodeDetails, err := nodeDetailsDB.GetNodeDetails(nodeID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get node details")
	}

	// If node details doesn't exist, return error
	if nodeDetails == nil {
		return nil, rmerror.NewRMError(nil, "Node has no config")
	}

	// Get config data from node details
	config, err := nodeDetails.GetServiceData(s.GetName())
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get config data")
	}

	// If config data doesn't exist, return error
	if config == nil {
		return nil, rmerror.NewRMError(nil, "Node has no config")
	}

	return config, nil
}

// Put writes config for a pure Matter node, which has no firmware to publish its own. It is
// refused for any node that is not pure Matter so an app cannot overwrite a real device's config.
func (s *ConfigService) Put(rmngCtx *rmngctx.RmngContext, nodeID string, data interface{}) error {
	groupNode, err := group_node_db.NewGroupNodeDB(rmngCtx).GetGroupNodeByNodeID(nodeID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to look up node for config write")
	}
	if groupNode == nil || !group.IsPureMatterNode(groupNode.Capabilities) {
		return ErrNotPureMatter
	}

	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	return nodeDetailsDB.SetServiceDataForNode(nodeID, s.GetName(), data)
}

// Delete is not allowed for config service
func (s *ConfigService) Delete(rmngCtx *rmngctx.RmngContext, nodeID string) error {
	return rmerror.NewRMError(nil, "DELETE operation not allowed for config service")
}

// SetNodeConfig sets the node configuration. This can only be called by a node.
func (s *ConfigService) SetNodeConfig(rmngCtx *rmngctx.RmngContext, data interface{}) error {
	// Only nodes can set their own config
	if reflect.TypeOf(rmngCtx.GetAccessor()).String() != "*node.Node" {
		return rmerror.NewRMError(nil, "only nodes can set their own config")
	}

	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	return nodeDetailsDB.UpdateServiceData(s.GetName(), data)
}

// DeleteNodeConfig deletes the node configuration. This can only be called by a node.
func (s *ConfigService) DeleteNodeConfig(rmngCtx *rmngctx.RmngContext) error {
	// Only nodes can delete their own config
	if reflect.TypeOf(rmngCtx.GetAccessor()).String() != "*node.Node" {
		return rmerror.NewRMError(nil, "only nodes can delete their own config")
	}

	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	nodeID := rmngCtx.GetID()
	return nodeDetailsDB.DeleteServiceData(nodeID, "config")
}

// Register registers the ConfigService with the global service registry
func Register() {
	service.Registry().RegisterNodeService(NewConfigService())
}

const propertyTimeSeries = "time_series"

func (nc NodeCfg) GetTimeSeriesParams() []timeseries_db.ParamKey {
	var params []timeseries_db.ParamKey

	// RMNG devices
	for _, device := range nc.Devices {
		for _, param := range device.Params {
			if exists, _ := collections.ItemExists(param.Properties, propertyTimeSeries); exists {
				params = append(params, timeseries_db.ParamKey{Name: param.ID, DataType: param.DataType})
			}
		}
	}

	// Matter endpoints not supported as we don't allow sending

	return params
}
