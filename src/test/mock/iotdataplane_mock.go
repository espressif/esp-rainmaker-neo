// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils"

	"github.com/aws/aws-sdk-go-v2/service/iotdataplane"
	"github.com/aws/smithy-go"
)

type AwsNodeShadow struct {
	State     *AwsShadowState `json:"state,omitempty"`
	Metadata  *AwsMetadata    `json:"metadata,omitempty"`
	Version   *int            `json:"version,omitempty"`
	Timestamp *int            `json:"timestamp,omitempty"`
}

type AwsMetadata struct {
	Reported map[string]interface{} `json:"reported,omitempty"`
	Desired  map[string]interface{} `json:"desired,omitempty"`
}

type AwsShadowState struct {
	Reported map[string]interface{} `json:"reported,omitempty"`
	Desired  map[string]interface{} `json:"desired,omitempty"`
}

// IoTDataPlaneMock is a mock implementation of the IoT Data Plane client
type IoTDataPlaneMock struct {
	mu sync.Mutex
	// Shadows stores the shadow data for each thing
	// The first key is the thing name, the second key is the shadow name
	Shadows map[string]map[string][]byte
	// PublishCalls stores the parameters of Publish calls
	PublishCalls []iotdataplane.PublishInput
}

// NewIoTDataPlaneMock creates a new IoTDataPlaneMock
func NewIoTDataPlaneMock() *IoTDataPlaneMock {
	return &IoTDataPlaneMock{
		Shadows:      make(map[string]map[string][]byte),
		PublishCalls: []iotdataplane.PublishInput{},
	}
}

func (m *IoTDataPlaneMock) Print() {
	for thingName, shadows := range m.Shadows {
		fmt.Printf("Thing: %s\n", thingName)
		for shadowName, payload := range shadows {
			fmt.Printf("  Shadow: %s\n", shadowName)
			var shadow AwsNodeShadow
			err := json.Unmarshal(payload, &shadow)
			if err != nil {
				fmt.Printf("  Payload: %s\n", string(payload))
			} else {
				fmt.Printf("  Payload: %v\n", shadow)
			}
		}
	}
}

func (m *IoTDataPlaneMock) GetNamedShadowNames(thingName string) []string {
	shadowNames := make([]string, 0, len(m.Shadows[thingName]))
	for shadowName := range m.Shadows[thingName] {
		shadowNames = append(shadowNames, shadowName)
	}
	return shadowNames
}

// UpdateThingShadow mocks the UpdateThingShadow operation
func (m *IoTDataPlaneMock) UpdateThingShadow(ctx context.Context, params *iotdataplane.UpdateThingShadowInput, optFns ...func(*iotdataplane.Options)) (*iotdataplane.UpdateThingShadowOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	thingName := *params.ThingName
	shadowName := *params.ShadowName

	if m.Shadows[thingName] == nil {
		m.Shadows[thingName] = make(map[string][]byte)
	}

	// Merge the new shadow data with existing data
	var existingShadow AwsNodeShadow
	if existing, ok := m.Shadows[thingName][shadowName]; ok {
		if err := json.Unmarshal(existing, &existingShadow); err != nil {
			return nil, fmt.Errorf("failed to unmarshal existing shadow data: %w", err)
		}
	}

	var newShadow AwsNodeShadow
	if err := json.Unmarshal(params.Payload, &newShadow); err != nil {
		return nil, fmt.Errorf("failed to unmarshal new shadow data: %w", err)
	}

	// Merge the new shadow data into the existing shadow
	if existingShadow.State == nil {
		existingShadow.State = newShadow.State
	} else if newShadow.State != nil {
		if newShadow.State.Reported != nil {
			if existingShadow.State.Reported == nil {
				existingShadow.State.Reported = newShadow.State.Reported
			} else {
				mergeShadowParams(existingShadow.State.Reported, newShadow.State.Reported)
			}
		}
		if newShadow.State.Desired != nil {
			if existingShadow.State.Desired == nil {
				existingShadow.State.Desired = newShadow.State.Desired
			} else {
				mergeShadowParams(existingShadow.State.Desired, newShadow.State.Desired)
			}
		}
	}

	if existingShadow.Version == nil {
		existingShadow.Version = newShadow.Version
	} else if newShadow.Version != nil {
		existingShadow.Version = newShadow.Version
	}

	// Marshal the merged data back to JSON
	updatedShadow, err := json.Marshal(existingShadow)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated shadow data: %w", err)
	}

	m.Shadows[thingName][shadowName] = updatedShadow

	return &iotdataplane.UpdateThingShadowOutput{
		Payload: updatedShadow,
	}, nil
}

// Helper function to merge shadow params
func mergeShadowParams(existing, new map[string]interface{}) {
	if existing == nil || new == nil {
		return
	}
	for k, v := range new {
		switch v := v.(type) {
		case map[string]interface{}:
			if existing[k] == nil {
				existing[k] = make(map[string]interface{})
			}
			if existingMap, ok := existing[k].(map[string]interface{}); ok {
				mergeShadowParams(existingMap, v)
			} else {
				existing[k] = v
			}
		default:
			existing[k] = v
		}
	}
}

// GetThingShadow mocks the GetThingShadow operation
func (m *IoTDataPlaneMock) GetThingShadow(ctx context.Context, params *iotdataplane.GetThingShadowInput, optFns ...func(*iotdataplane.Options)) (*iotdataplane.GetThingShadowOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	thingName := *params.ThingName
	shadowName := *params.ShadowName

	if m.Shadows[thingName] == nil || m.Shadows[thingName][shadowName] == nil {
		return nil, &smithy.GenericAPIError{
			Code:    "ResourceNotFoundException",
			Message: "No shadow exists with name: " + shadowName,
		}
	}

	return &iotdataplane.GetThingShadowOutput{
		Payload: m.Shadows[thingName][shadowName],
	}, nil
}

func (m *IoTDataPlaneMock) DeleteThingShadow(ctx context.Context, params *iotdataplane.DeleteThingShadowInput, optFns ...func(*iotdataplane.Options)) (*iotdataplane.DeleteThingShadowOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.Shadows[*params.ThingName], *params.ShadowName)

	return &iotdataplane.DeleteThingShadowOutput{}, nil
}

// Publish mocks the Publish operation
func (m *IoTDataPlaneMock) Publish(ctx context.Context, params *iotdataplane.PublishInput, optFns ...func(*iotdataplane.Options)) (*iotdataplane.PublishOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store the Publish call parameters
	m.PublishCalls = append(m.PublishCalls, *params)

	return &iotdataplane.PublishOutput{}, nil
}

// AddDirect directly adds a shadow to the mock storage
func (m *IoTDataPlaneMock) AddDirect(thingName, shadowName string, payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Shadows[thingName] == nil {
		m.Shadows[thingName] = make(map[string][]byte)
	}
	m.Shadows[thingName][shadowName] = payload
}

// GetDirect directly retrieves a shadow from the mock storage
func (m *IoTDataPlaneMock) GetDirect(thingName, shadowName string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Shadows[thingName] == nil || m.Shadows[thingName][shadowName] == nil {
		return nil, fmt.Errorf("shadow not found for thing %s and shadow name %s", thingName, shadowName)
	}

	return m.Shadows[thingName][shadowName], nil
}

func (m *IoTDataPlaneMock) verifyShadowTags(nodeID string, expectedTags map[string]string, tagType node.TagType) bool {
	if _, ok := m.Shadows[nodeID]; !ok {
		return false
	}
	if _, ok := m.Shadows[nodeID]["iparams"]; !ok {
		return false
	}

	shadowJSON := m.Shadows[nodeID]["iparams"]

	// Parse the shadow JSON into IoTNodeShadow struct
	var nodeShadow node.IoTNodeShadow
	if err := json.Unmarshal(shadowJSON, &nodeShadow); err != nil {
		return false
	}

	// Check if required fields exist
	if nodeShadow.State == nil || nodeShadow.State.Reported == nil || nodeShadow.State.Reported.Data == nil {
		return false
	}

	// Determine the section based on tag type
	var tagsObj *node.TagsObj
	switch tagType {
	case node.TagTypeAdmin:
		tagsObj = nodeShadow.State.Reported.Data.Admin
	case node.TagTypeDevice:
		tagsObj = nodeShadow.State.Reported.Data.Device
	case node.TagTypeUser:
		tagsObj = nodeShadow.State.Reported.Data.User
	}

	// Check if the tags object exists
	if tagsObj == nil || tagsObj.Tags == nil {
		return false
	}

	// Check each expected tag
	for key, expectedValue := range expectedTags {
		actualValue, exists := tagsObj.Tags[key]
		if !exists {
			utils.Rlog.Error().Str("node_id", nodeID).Str("key", key).Str("expected_value", expectedValue).Str("tags", fmt.Sprintf("%v", tagsObj.Tags)).Msg("Tag not found")
			return false
		}

		// Convert actualValue to string for comparison
		actualValueStr, ok := actualValue.(string)
		if !ok || actualValueStr != expectedValue {
			return false
		}
	}

	return true
}

func (m *IoTDataPlaneMock) VerifyTags(nodeID string, adminTags, deviceTags, userTags map[string]string) bool {
	if len(adminTags) > 0 && !m.verifyShadowTags(nodeID, adminTags, node.TagTypeAdmin) {
		return false
	}
	if len(deviceTags) > 0 && !m.verifyShadowTags(nodeID, deviceTags, node.TagTypeDevice) {
		return false
	}
	if len(userTags) > 0 && !m.verifyShadowTags(nodeID, userTags, node.TagTypeUser) {
		return false
	}
	return true
}
