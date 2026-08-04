// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// NodeService defines the interface for node-specific services
type NodeService interface {
	// GetName returns the unique name of the service
	GetName() string

	// HasVersion returns whether this service supports versioning
	HasVersion() bool

	// Get retrieves service data for a node
	Get(rmngCtx *rmngctx.RmngContext, nodeID string) (interface{}, error)

	// Put updates service data for a node
	Put(rmngCtx *rmngctx.RmngContext, nodeID string, data interface{}) error

	// Delete removes service data for a node
	Delete(rmngCtx *rmngctx.RmngContext, nodeID string) error
}

// GroupService defines the interface for group-specific services
type GroupService interface {
	// GetName returns the unique name of the service
	GetName() string

	// HasVersion returns whether this service supports versioning
	HasVersion() bool

	// Get retrieves service data for a group
	Get(rmngCtx *rmngctx.RmngContext, groupID string) (interface{}, error)

	// Put updates service data for a group and returns result data
	Put(rmngCtx *rmngctx.RmngContext, groupID string, data interface{}) (interface{}, error)

	// Delete removes service data for a group
	Delete(rmngCtx *rmngctx.RmngContext, groupID string) error

	// SupportsResourceID returns whether this service supports resource IDs
	SupportsResourceID() bool

	// GetWithResourceID retrieves a specific resource within a group
	GetWithResourceID(rmngCtx *rmngctx.RmngContext, groupID string, resourceID string) (interface{}, error)

	// PutWithResourceID updates a specific resource within a group and returns result data
	PutWithResourceID(rmngCtx *rmngctx.RmngContext, groupID string, resourceID string, data interface{}) (interface{}, error)

	// DeleteWithResourceID removes a specific resource within a group
	DeleteWithResourceID(rmngCtx *rmngctx.RmngContext, groupID string, resourceID string) error
}

// BaseService provides a basic implementation of common service functionality
// that can be embedded in service-specific implementations
type BaseService struct {
	Name      string
	Versioned bool
}

// GetName returns the name of the service
func (s *BaseService) GetName() string {
	return s.Name
}

// HasVersion returns whether this service supports versioning
func (s *BaseService) HasVersion() bool {
	return s.Versioned
}

// SupportsResourceID returns whether this service supports resource IDs
// Default implementation returns false, services should override if they support resource IDs
func (s *BaseService) SupportsResourceID() bool {
	return false
}

// GetWithResourceID provides a default implementation that returns an error
// Services that support resource IDs should override this method
func (s *BaseService) GetWithResourceID(rmngCtx *rmngctx.RmngContext, groupID string, resourceID string) (interface{}, error) {
	return nil, rmerror.NewRMError(nil, "service does not support resource IDs")
}

// PutWithResourceID provides a default implementation that returns an error
// Services that support resource IDs should override this method
func (s *BaseService) PutWithResourceID(rmngCtx *rmngctx.RmngContext, groupID string, resourceID string, data interface{}) (interface{}, error) {
	return nil, rmerror.NewRMError(nil, "service does not support resource IDs")
}

// DeleteWithResourceID provides a default implementation that returns an error
// Services that support resource IDs should override this method
func (s *BaseService) DeleteWithResourceID(rmngCtx *rmngctx.RmngContext, groupID string, resourceID string) error {
	return rmerror.NewRMError(nil, "service does not support resource IDs")
}

// ServiceRegistry maintains a map of registered services
type ServiceRegistry struct {
	nodeServices  map[string]NodeService
	groupServices map[string]GroupService
}

// NewServiceRegistry creates a new ServiceRegistry
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		nodeServices:  make(map[string]NodeService),
		groupServices: make(map[string]GroupService),
	}
}

// RegisterNodeService adds a node service to the registry
func (r *ServiceRegistry) RegisterNodeService(service NodeService) {
	r.nodeServices[service.GetName()] = service
}

// RegisterGroupService adds a group service to the registry
func (r *ServiceRegistry) RegisterGroupService(service GroupService) {
	r.groupServices[service.GetName()] = service
}

// GetService retrieves any service by name (first checks node services, then group services)
func (r *ServiceRegistry) GetService(name string) (interface{}, error) {
	// First check node services
	if service, ok := r.nodeServices[name]; ok {
		return service, nil
	}

	// Then check group services
	if service, ok := r.groupServices[name]; ok {
		return service, nil
	}

	return nil, rmerror.NewRMError(nil, "service not found: "+name)
}

// GetNodeService retrieves a node service by name
func (r *ServiceRegistry) GetNodeService(name string) (NodeService, error) {
	service, ok := r.nodeServices[name]
	if !ok {
		return nil, rmerror.NewRMError(nil, "node service not found: "+name)
	}
	return service, nil
}

// GetGroupService retrieves a group service by name
func (r *ServiceRegistry) GetGroupService(name string) (GroupService, error) {
	service, ok := r.groupServices[name]
	if !ok {
		return nil, rmerror.NewRMError(nil, "group service not found: "+name)
	}
	return service, nil
}

// GetAllNodeServices returns all registered node services
func (r *ServiceRegistry) GetAllNodeServices() map[string]NodeService {
	return r.nodeServices
}

// GetAllGroupServices returns all registered group services
func (r *ServiceRegistry) GetAllGroupServices() map[string]GroupService {
	return r.groupServices
}

// GetAllServices returns all registered services (both node and group)
func (r *ServiceRegistry) GetAllServices() map[string]interface{} {
	allServices := make(map[string]interface{})

	// Add all node services
	for name, service := range r.nodeServices {
		allServices[name] = service
	}

	// Add all group services
	for name, service := range r.groupServices {
		allServices[name] = service
	}

	return allServices
}

// global service registry
var registry *ServiceRegistry

// Initialize creates the global service registry
func Initialize() {
	registry = NewServiceRegistry()
}

// Registry returns the global service registry
func Registry() *ServiceRegistry {
	if registry == nil {
		Initialize()
	}
	return registry
}
