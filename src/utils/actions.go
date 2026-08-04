// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package utils

// GroupActions are the actions that can be performed on a group
// These actions are only applicable to the parent group, not the sub-groups
// As of now no RBAC is applied to the sub-groups, so no actions are defined for them. They always
// operate under the permissions of the parent group
type GroupActions string

const (
	// All
	GroupAll GroupActions = "group:*"
	// Create a Group
	GroupCreate GroupActions = "group:create"
	// Create a SubGroup within a Group
	GroupCreateSubGroup GroupActions = "group:createsubgroup"
	// List or Get a Group
	GroupGet GroupActions = "group:get"
	// List or Get a SubEntity within a Group
	GroupListSubEntities GroupActions = "group:listsubentities"
	// Add/Remove a Node to/from a Group
	GroupEditNodes GroupActions = "group:editnodes"
	// Update a Group (e.g., rename, services)
	GroupUpdate GroupActions = "group:update"
	// Update a SubGroup (e.g., rename)
	GroupUpdateSubGroup GroupActions = "group:updatesubgroup"
	// Enable/Update capabilities on a Group (e.g., convert to a Matter fabric)
	GroupUpdateCapabilities GroupActions = "group:updatecapabilities"
	// Delete a Group
	GroupDelete GroupActions = "group:delete"
	// Delete a SubGroup
	GroupDeleteSubGroup GroupActions = "group:deletesubgroup"
	// Share a Group with a User
	GroupShare GroupActions = "group:share"
	// List Users who have access to a Group
	GroupListUsers GroupActions = "group:listusers"
	// List Users who have primary access to a Group
	GroupListPrimaryUsers GroupActions = "group:listprimaryusers"
	// Automation-specific permissions
	GroupGetAutomation    GroupActions = "group:getautomation"
	GroupEditAutomation   GroupActions = "group:editautomation"
	GroupDeleteAutomation GroupActions = "group:deleteautomation"
)

func (g GroupActions) String() string {
	return string(g)
}

type GroupAccessType string

const (
	GroupPrimaryAccess   GroupAccessType = "primary"
	GroupSecondaryAccess GroupAccessType = "secondary"
	GroupSubEntityAccess GroupAccessType = "subentity"
)

func GetGroupPermissions(accessType GroupAccessType) []string {
	switch accessType {
	case GroupPrimaryAccess:
		return []string{GroupAll.String()}
	case GroupSecondaryAccess:
		// No delete or share permissions
		return []string{GroupCreateSubGroup.String(), GroupGet.String(), GroupListSubEntities.String(), GroupUpdate.String(), GroupUpdateSubGroup.String(), GroupGetAutomation.String(), GroupEditAutomation.String(), GroupDeleteAutomation.String(), GroupDeleteSubGroup.String(), GroupListPrimaryUsers.String()}
	case GroupSubEntityAccess:
		return []string{GroupListSubEntities.String(), GroupUpdateSubGroup.String(), GroupListPrimaryUsers.String()}
	default:
		return []string{}
	}
}

type NodeActions string

const (
	// All
	NodeAll NodeActions = "node:*"
	// Get a Node
	NodeGet NodeActions = "node:get"
	// Node update (except config and sadow)
	NodeUpdate NodeActions = "node:update"
	// Delete a Node Config
	NodeDeleteConfig NodeActions = "node:deleteconfig"
	// Put a Node Config
	NodePutConfig NodeActions = "node:putconfig"
	// Add a Node to a Group
	NodeEditGroups NodeActions = "node:editgroups"
	// Write to the reported shadow of a Node
	NodeWriteShadow NodeActions = "node:writeshadow"
	// Read from the shadow of a Node
	NodeReadShadow NodeActions = "node:readshadow"
	// Publish to Device's topics
	NodePublishToDevice NodeActions = "node:publishtodevice"
)

func (n NodeActions) String() string {
	return string(n)
}

type NodeAdminActions string

const (
	// All
	NodeAdminAll NodeAdminActions = "nodeadmin:*"
	// Add a Node
	NodeAdminAdd NodeAdminActions = "nodeadmin:add"
	// Node regsiter status
	NodeAdminRegisterStatus NodeAdminActions = "nodeadmin:registerstatus"
	// Create a node-ID reservation for a claim key.
	// Resource is the reservation's mac_addr.
	NodeAdminReserveID NodeAdminActions = "nodeadmin:reserveid"
	// Read a single node-ID reservation, keyed by its device.
	// Resource is the reservation's mac_addr.
	NodeAdminGetReservation NodeAdminActions = "nodeadmin:getreservation"
	// Count a claimant's node-ID reservations for quota enforcement. This is a
	// claimant-scoped operation spanning all of the claimant's devices, so the
	// resource is the claimant_id — not a mac_addr, unlike the two above.
	NodeAdminCountReservations NodeAdminActions = "nodeadmin:countreservations"
)

func (na NodeAdminActions) String() string {
	return string(na)
}

// AdminConfigActions gate access to the rmng_admin_config table — the
// shared store for runtime-set admin configuration that needs to survive
// CloudFormation redeploys (see docs/en/specs/iot_event_mode.md §4.4 and
// rmneo/handlers/admin/admin_config_base.py). Resource is the row's config_key
// (e.g. "iot_event_mode"); writers must hold AdminConfigSet, readers
// AdminConfigGet. Granted exclusively to SystemActor and superAdmin
// callers — this table is never user-facing.
type AdminConfigActions string

const (
	// All
	AdminConfigAll AdminConfigActions = "adminconfig:*"
	// Read a config row
	AdminConfigGet AdminConfigActions = "adminconfig:get"
	// Write a config row
	AdminConfigSet AdminConfigActions = "adminconfig:set"
)

func (a AdminConfigActions) String() string {
	return string(a)
}
