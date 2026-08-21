// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

// ErrNoGroupInName marks a name carrying no group ID — the bare "params-",
// emitted when a publisher builds it from an empty groupId. Group membership is
// what resolves recipients, so such a notification is unroutable: a no-op, not
// a failure.
var ErrNoGroupInName = errors.New("no group ID in name")

// NotificationServiceType represents the type of notification service
type NotificationServiceType string

const (
	// NotificationServiceTypeUserSpecific represents notifications specific to a user
	NotificationServiceTypeUserSpecific NotificationServiceType = "user_specific"
	// NotificationServiceTypeGeneric represents notifications that are not user specific
	NotificationServiceTypeGeneric NotificationServiceType = "generic"
)

// NotificationService defines the interface for notification operations
type NotificationService interface {
	// GetName returns the name of the notification service
	GetName() string

	// GetType returns the type of the notification service
	GetType() NotificationServiceType

	// Send sends a notification
	Send(notificationData interface{}) error

	// SendTo sends notification to the user list
	SendTo(notificationData interface{}, userIDs []string) error

	// Data Marshaling
	Marshal(notification *Notification) (interface{}, error)
}

// ConnectivityNotifier marks a notification service that also wants shadow
// events whose only change is the node's connectivity. shadow_notify_rule
// fires on reported.online transitions without a notify.version bump, and
// such an event still carries the node's lingering notify map; dispatching
// every service on it would re-deliver the last notification on every online
// flip. Services that do not implement this interface (or return false) are
// skipped for connectivity-only events.
type ConnectivityNotifier interface {
	NotifyOnConnectivityChange() bool
}

// NotificationServiceRegistry is a registry for notification services
type NotificationServiceRegistry struct {
	services map[string]NotificationService
}

// NewNotificationServiceRegistry creates a new NotificationServiceRegistry
func NewNotificationServiceRegistry() *NotificationServiceRegistry {
	return &NotificationServiceRegistry{
		services: make(map[string]NotificationService),
	}
}

// Register registers a notification service with the registry
func (r *NotificationServiceRegistry) Register(service NotificationService) {
	r.services[service.GetName()] = service
}

// Get retrieves a notification service from the registry
func (r *NotificationServiceRegistry) Get(name string) (NotificationService, error) {
	service, ok := r.services[name]
	if !ok {
		return nil, rmerror.NewRMError(nil, "notification service not found: "+name)
	}
	return service, nil
}

var notificationServiceRegistry *NotificationServiceRegistry

func Initialize() {
	notificationServiceRegistry = NewNotificationServiceRegistry()
}

func Registry() *NotificationServiceRegistry {
	return notificationServiceRegistry
}

type NotificationType string

const (
	NotificationTypeShadowUpdate    NotificationType = "shadow_update"
	NotificationTypeDirect          NotificationType = "direct"
	NotificationTypeGroupMembership NotificationType = "group_membership_change"
)

// Group membership change actions. A control-plane add/remove of a node to/from
// a group percolates one of these to the delivery channels so voice assistants
// can re-discover (add) or drop (remove) the device.
const (
	GroupMembershipActionAdded   = "added"
	GroupMembershipActionRemoved = "removed"
)

// ShadowUpdateNotification is the data that is received for a shadow update notification
type ShadowUpdateNotification struct {
	NodeID     string
	ShadowName string
	Delta      node.ReportedOrDesiredShadow
	State      node.ReportedOrDesiredShadow
}

// DirectNotification is the data that is received for a direct notification
type DirectNotification struct {
	NodeID     string
	NotifyData map[string]interface{}
}

// GroupMembershipNotification is the data for a node's group membership
// change. Unlike shadow/direct notifications, the group is known directly
// (the backend just performed the association), not parsed from a topic name.
type GroupMembershipNotification struct {
	NodeID string
	Action string // GroupMembershipActionAdded or GroupMembershipActionRemoved
}

// Notification is a notification of any type
type Notification struct {
	NotificationType       NotificationType
	ShadowUpdateData       *ShadowUpdateNotification
	DirectNotificationData *DirectNotification
	GroupMembershipData    *GroupMembershipNotification
	// Group information parsed from shadow name or notify topic name
	GroupID     string
	SubGroupIDs []string
	TopicName   string // Original shadow name or notify topic name
}

// NewShadowUpdateNotification creates a new shadow update notification
func NewShadowUpdateNotification(nodeID, shadowName string, prevState node.ReportedOrDesiredShadow, currState node.ReportedOrDesiredShadow) (*Notification, error) {
	delta, err := node.ComputeJSONDeltaMap(prevState, currState)
	if err != nil {
		rlog.Error(context.TODO()).Err(err).Msgf("Failed to compute JSON delta: %s", err)
		return nil, err
	}
	rlog.Info(context.TODO()).Interface("delta", delta).Msg("Delta")

	// Make sure that shadowName has "params-" prefix
	if !strings.HasPrefix(shadowName, "params-") {
		return nil, rmerror.NewRMError(nil, "shadow name must have 'params-' prefix: "+shadowName)
	}

	// Parse group information from shadow name
	groupID, subGroupIDs, err := parseGroupIDFromPartialName(shadowName[7:])
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse group information from shadow name: "+shadowName)
	}

	return &Notification{
		NotificationType: NotificationTypeShadowUpdate,
		ShadowUpdateData: &ShadowUpdateNotification{
			NodeID:     nodeID,
			ShadowName: shadowName,
			Delta:      delta,
			State:      currState,
		},
		GroupID:     groupID,
		SubGroupIDs: subGroupIDs,
		TopicName:   shadowName,
	}, nil
}

// NewDirectNotification creates a new direct notification
func NewDirectNotification(nodeID string, notifyTopicName string, notifyData map[string]interface{}) (*Notification, error) {
	groupID, subGroupIDs, err := parseGroupIDFromPartialName(notifyTopicName)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse group information from notify topic name: "+notifyTopicName)
	}

	return &Notification{
		NotificationType: NotificationTypeDirect,
		DirectNotificationData: &DirectNotification{
			NodeID:     nodeID,
			NotifyData: notifyData,
		},
		GroupID:     groupID,
		SubGroupIDs: subGroupIDs,
		TopicName:   notifyTopicName,
	}, nil
}

// NewGroupMembershipNotification creates a notification for a node being added
// to or removed from a group. The group/subgroup are supplied directly by the
// backend (the association just happened) rather than parsed from a topic name,
// so this does not go through parseGroupIDFromPartialName.
func NewGroupMembershipNotification(nodeID, groupID string, subGroupIDs []string, action string) (*Notification, error) {
	if nodeID == "" || groupID == "" {
		return nil, rmerror.NewRMError(nil, "nodeID and groupID are required for group membership notification")
	}
	if action != GroupMembershipActionAdded && action != GroupMembershipActionRemoved {
		return nil, rmerror.NewRMError(nil, "invalid group membership action: "+action)
	}
	if subGroupIDs == nil {
		subGroupIDs = []string{}
	}

	return &Notification{
		NotificationType: NotificationTypeGroupMembership,
		GroupMembershipData: &GroupMembershipNotification{
			NodeID: nodeID,
			Action: action,
		},
		GroupID:     groupID,
		SubGroupIDs: subGroupIDs,
	}, nil
}

// GetGroupInfo returns the parsed group and subgroup information from the notification
// Returns groupID, subGroupIDs, and topicName used for group extraction
func (n *Notification) GetGroupInfo() (string, []string, string, error) {
	// Group information is already parsed and stored during construction
	return n.GroupID, n.SubGroupIDs, n.TopicName, nil
}

// NewNotificationFromEvent creates a notification from event data based on notification type
func NewNotificationFromEvent(nodeID, topicName, notificationType string, currState, prevState node.ReportedOrDesiredShadow, notify map[string]interface{}) (*Notification, error) {
	switch notificationType {
	case "shadow_update":
		// For shadow updates, currState is required (prevState can be nil for first update)
		// Note: We check currState.Params since ReportedOrDesiredShadow is a struct, not a map
		if currState.Params == nil && currState.Data == nil && currState.Online == nil {
			return nil, rmerror.NewRMError(nil, "currState is required for shadow update notifications")
		}
		return NewShadowUpdateNotification(nodeID, topicName, prevState, currState)
	case "direct_notification":
		// For direct notifications, notify data is required
		if notify == nil {
			return nil, rmerror.NewRMError(nil, "notify data is required for direct notifications")
		}
		return NewDirectNotification(nodeID, topicName, notify)
	default:
		return nil, rmerror.NewRMError(nil, "unknown notification type: "+notificationType)
	}
}

// parseGroupIDFromPartialName parses group and subgroup IDs from partial name
// This is a simplified version that doesn't import the node package to avoid circular dependencies
func parseGroupIDFromPartialName(partialName string) (string, []string, error) {
	if partialName == "" {
		return "", nil, rmerror.NewRMError(ErrNoGroupInName, "empty partial name")
	}

	parts := strings.Split(partialName, "-")

	// First part is always the group ID
	groupID := parts[0]

	// Remaining parts are subgroup IDs (if any)
	subGroupIDs := []string{}
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" { // Skip empty parts
			subGroupIDs = append(subGroupIDs, parts[i])
		}
	}

	if groupID == "" {
		return "", nil, rmerror.NewRMError(ErrNoGroupInName, "could not extract group ID from partial name: "+partialName)
	}

	return groupID, subGroupIDs, nil
}
