// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	. "github.com/onsi/gomega"
)

func GetPublishedData(testNode *node.Node, sub_topic string) []byte {
	iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
	topic := fmt.Sprintf("rainmaker/nodes/%s/user/%s/params", testNode.GetID(), sub_topic)
	for _, call := range iotDataClient.PublishCalls {
		if *call.Topic == topic {
			return call.Payload
		}
	}
	return nil
}

func GetPublishedDataForNodeGroup(testNode *node.Node, groups group_node_db.NodesGroups) []byte {
	return GetPublishedData(testNode, node.GetShadowNameForNodeGroups(groups))
}

// GetShadowForNodeGroup returns the shadow state for the given node and groups
func GetShadowForNodeGroup(testNode *node.Node, groups group_node_db.NodesGroups) node.IoTNodeShadow {
	iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
	newShadow, err := iotDataClient.GetDirect(testNode.GetID(), node.GetShadowNameForNodeGroups(groups))
	Expect(err).To(BeNil())

	var shadow node.IoTNodeShadow
	err = json.Unmarshal(newShadow, &shadow)
	Expect(err).To(BeNil())
	return shadow
}

// SetupShadow sets up the shadow for the given node and groups
func SetupShadow(nodeId string, shadowState node.IoTNodeShadow, groups group_node_db.NodesGroups) {
	iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
	shadowJSON, _ := json.Marshal(shadowState)
	iotDataClient.AddDirect(nodeId, node.GetShadowNameForNodeGroups(groups), shadowJSON)
}

// ConvertAllFloatToInt converts all float64 values to ints if they are whole numbers
// This is necessary because of floating point and integer comparison issues
func ConvertAllFloatToInt(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			v[key] = ConvertAllFloatToInt(value)
		}
		return v
	case []interface{}:
		for i, value := range v {
			v[i] = ConvertAllFloatToInt(value)
		}
		return v
	case float64:
		if v == float64(int(v)) {
			return int(v)
		}
		return v
	case *node.ShadowState:
		if v == nil {
			return v
		}
		if v.Reported != nil {
			v.Reported = ConvertAllFloatToInt(v.Reported).(*node.ReportedOrDesiredShadow)
		}
		if v.Desired != nil {
			v.Desired = ConvertAllFloatToInt(v.Desired).(*node.ReportedOrDesiredShadow)
		}
		return v
	case *node.ReportedOrDesiredShadow:
		if v == nil {
			return v
		}
		if v.Params != nil {
			v.Params = ConvertAllFloatToInt(v.Params).(map[string]interface{})
		}
		return v
	case node.IoTNodeShadow:
		if v.State != nil {
			v.State = ConvertAllFloatToInt(v.State).(*node.ShadowState)
		}
		return v
	default:
		return v
	}
}
