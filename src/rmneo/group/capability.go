// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group

import "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

// GroupCapability defines the interface that all group capabilities must implement.
// Capabilities extend group functionality with specific features like Matter fabric support.
type GroupCapability interface {
	// Name returns the unique identifier for this capability (e.g., "matter")
	Name() string

	// OnGroupCreate is called when a group with this capability is created.
	// It generates and returns capability-specific data to be stored in the database.
	OnGroupCreate(ctx *rmngctx.RmngContext, group *Group) (*CapabilityData, error)

	// OnGroupDelete is called when a group with this capability is deleted.
	// It can be used to clean up capability-specific resources.
	OnGroupDelete(ctx *rmngctx.RmngContext, groupID string) error

	// OnUserJoinGroup is called when userID joins a group with this capability: at group
	// creation, on sharing approval, or for each existing member when the capability is
	// added to an existing group. It can be used to create capability-specific user data.
	OnUserJoinGroup(ctx *rmngctx.RmngContext, groupID string, userID string) error

	// OnUserExitGroup is called when a user exits/leaves a group with this capability.
	// This is triggered when a user is unshared from a group.
	// accessType indicates the user's access level (primary/secondary) at exit time.
	OnUserExitGroup(ctx *rmngctx.RmngContext, groupID string, userID string, accessType string) error

	// GetResponseData returns data to include in API responses for this capability.
	GetResponseData(ctx *rmngctx.RmngContext, group *Group) (map[string]interface{}, error)
}

// CapabilityData holds capability-specific data for database storage
type CapabilityData struct {
	// DBFields contains key-value pairs to be stored in the groups table
	DBFields map[string]interface{}
}

// capabilityRegistry holds all registered capability handlers
var capabilityRegistry = make(map[string]GroupCapability)

// RegisterCapability registers a capability handler.
// This is typically called in init() functions of capability implementations.
func RegisterCapability(cap GroupCapability) {
	capabilityRegistry[cap.Name()] = cap
}

// GetCapability returns the capability handler for the given name.
// Returns nil and false if the capability is not registered.
func GetCapability(name string) (GroupCapability, bool) {
	cap, ok := capabilityRegistry[name]
	return cap, ok
}

// GetRegisteredCapabilities returns the names of all registered capabilities.
func GetRegisteredCapabilities() []string {
	names := make([]string, 0, len(capabilityRegistry))
	for name := range capabilityRegistry {
		names = append(names, name)
	}
	return names
}

// ValidateCapabilities checks if all provided capability names are registered.
// Returns an error if any capability is unknown.
func ValidateCapabilities(capabilities []string) error {
	for _, capName := range capabilities {
		if _, ok := capabilityRegistry[capName]; !ok {
			return &UnknownCapabilityError{Name: capName}
		}
	}
	return nil
}

// UnknownCapabilityError is returned when an unknown capability is requested
type UnknownCapabilityError struct {
	Name string
}

func (e *UnknownCapabilityError) Error() string {
	return "unknown capability: " + e.Name
}
