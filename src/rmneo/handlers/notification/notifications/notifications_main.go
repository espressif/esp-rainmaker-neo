// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/collections"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"maps"
	"os"
	"slices"

	"github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/gva"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/push"
	"github.com/espressif/esp-rainmaker-neo/src/smartthings"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"strings"

	"github.com/aws/aws-lambda-go/lambda"
)

// NotificationEvent represents the event received from IoT Core (device-originated
// shadow_update / direct_notification) or from a backend Lambda async-invoke
// (control-plane group_membership_change; see node.EmitGroupMembershipChangeAsync).
type NotificationEvent struct {
	NodeID           string                       `json:"node_id,omitempty"`
	TopicName        string                       `json:"topic_name,omitempty"`        // Common field for both shadow names and notify topic names
	NotificationType string                       `json:"notification_type,omitempty"` // "shadow_update", "direct_notification" or "group_membership_change"
	CurrState        node.ReportedOrDesiredShadow `json:"curr_state"`
	PrevState        node.ReportedOrDesiredShadow `json:"prev_state"`
	Notify           map[string]interface{}       `json:"notify"`
	// group_membership_change only: group is supplied directly (not parsed from a topic).
	GroupID     string   `json:"group_id,omitempty"`
	SubGroupIDs []string `json:"sub_group_ids,omitempty"`
	Action      string   `json:"action,omitempty"` // "added" or "removed"
}

// resolveMockBaseURL returns the mock base URL to register services against, or
// "" for production. The presence of webhook_mock_base_url is the switch: the
// integration-test fixture sets it to route notifications at the in-cloud mock;
// unset (the production default) sends them to the real Alexa/GVA endpoints.
func resolveMockBaseURL() string {
	return os.Getenv("webhook_mock_base_url")
}

func initialize() {
	notification.Initialize()
	// Register the automation service
	notification.Registry().Register(notification.NewAutomationService())

	mockBaseURL := resolveMockBaseURL()
	if mockBaseURL != "" {
		rlog.Info(context.TODO()).Msgf("Registering webhook mock service at %s", mockBaseURL)
		notification.Registry().Register(notification.NewWebhookMockService(mockBaseURL))
	}

	// Register Alexa notification service
	alexaAdapter := alexa_skill.NewAlexaNotification(context.Background(), mockBaseURL)
	notification.Registry().Register(alexaAdapter)
	rlog.Info(context.TODO()).Msg("Registered Alexa notification service")

	// Register GVA notification service
	gvaAdapter := gva.NewGVANotification(context.Background(), mockBaseURL)
	notification.Registry().Register(gvaAdapter)
	rlog.Info(context.TODO()).Msg("Registered GVA notification service")

	// Register SmartThings notification service
	stAdapter := smartthings.NewSTNotification(context.Background(), mockBaseURL)
	notification.Registry().Register(stAdapter)
	rlog.Info(context.TODO()).Msg("Registered SmartThings notification service")

	pushAdapter := push.NewMobilePushService()
	notification.Registry().Register(pushAdapter)
	rlog.Info(context.TODO()).Msg("Registered push notification service")

	// Register node_reset service: disassociates the node and runs data
	// cleanup when firmware reports a self factory-reset.
	notification.Registry().Register(notification.NewNodeResetService())
	rlog.Info(context.TODO()).Msg("Registered node_reset notification service")
}

// validateNodeGroupAlignment validates that the node belongs to the expected group and subgroups
func validateNodeGroupAlignment(rmngCtx *rmngctx.RmngContext, nodeID string, expectedGroupID string, expectedSubGroupIDs []string) error {
	// Get the node's actual group and subgroup information
	n := node.NewNode(nodeID)
	actualGroups, err := n.GetNodesGroups(rmngCtx)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get groups for node "+nodeID)
	}

	// Ensure group matches
	if actualGroups.Group != expectedGroupID {
		return rmerror.NewRMError(nil, "group mismatch: expected group "+expectedGroupID+" but node belongs to group "+actualGroups.Group)
	}

	// Ensure subgroups match (order doesn't matter)
	if !collections.SlicesEqual(actualGroups.SubGroups, expectedSubGroupIDs) {
		return rmerror.NewRMError(nil, "subgroup mismatch: expected subgroups "+strings.Join(expectedSubGroupIDs, ",")+" but node belongs to subgroups "+strings.Join(actualGroups.SubGroups, ","))
	}

	return nil
}

// notifyVersion returns the notify.version inside a reported shadow's params.
func notifyVersion(state node.ReportedOrDesiredShadow) (interface{}, bool) {
	notifyMap, ok := state.Params["notify"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	version, ok := notifyMap["version"]
	return version, ok
}

// notifyVersionChanged reports whether notify.version moved between the
// previous and current shadow documents. A missing version on either side
// cannot prove a lingering map, so it counts as changed (fail-open): the
// first-ever notification has no previous version, and a rule fire without a
// current version cannot be a notify bump in the first place.
func notifyVersionChanged(curr, prev node.ReportedOrDesiredShadow) bool {
	currVersion, currOk := notifyVersion(curr)
	prevVersion, prevOk := notifyVersion(prev)
	if !currOk || !prevOk {
		return true
	}
	return currVersion != prevVersion
}

// hasDispatchableService reports whether the notify map names any service.
// "version" is bookkeeping the dispatch loop skips, so a map holding only it
// dispatches nothing — same as an empty map.
func hasDispatchableService(notify map[string]interface{}) bool {
	for serviceName := range notify {
		if serviceName != "version" {
			return true
		}
	}
	return false
}

// hasConnectivityDispatchTarget gates the group-alignment DynamoDB read: a
// connectivity-only fire no service listens for dispatches nothing.
func hasConnectivityDispatchTarget(registry *notification.NotificationServiceRegistry, notify map[string]interface{}) bool {
	for serviceName := range notify {
		if serviceName == "version" {
			continue
		}
		service, err := registry.Get(serviceName)
		if err != nil {
			continue
		}
		if cn, ok := service.(notification.ConnectivityNotifier); ok && cn.NotifyOnConnectivityChange() {
			return true
		}
	}
	return false
}

func getUsersForNotification(rmngCtx *rmngctx.RmngContext, notif *notification.Notification) ([]string, error) {
	// Use the generic method to get group information
	groupID, subGroupIDs, topicName, err := notif.GetGroupInfo()
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Msgf("Failed to get group information from notification")
		return nil, err
	}

	rlog.Info(rmngCtx).Msgf("Getting users for notification for topic name: %s, Group ID: %s, Sub Group IDs: %v", topicName, groupID, subGroupIDs)
	usersMap, err := group.ListUsersForGroupOrSubGroup(rmngCtx, groupID, subGroupIDs)
	if err != nil {
		return nil, err
	}
	return slices.Collect(maps.Keys(usersMap)), nil
}

func Handler(ctx context.Context, event NotificationEvent) error {
	rlog.Info(ctx).Msgf("Received notification event: %+v", event)

	// Dispatch iterates over event.Notify skipping "version", so a map with no
	// service key cannot notify anyone — return before paying for construction
	// and the group-alignment DDB read.
	if !hasDispatchableService(event.Notify) {
		rlog.Debug(ctx).Msgf("No notify services in event for node %s; nothing to dispatch", event.NodeID)
		return nil
	}

	var notif *notification.Notification
	var err error
	var nodeID string

	// Create notification using the generic factory
	nodeID = event.NodeID
	rlog.Info(ctx).Msgf("Processing %s notification for node: %s, topic: %s", event.NotificationType, nodeID, event.TopicName)

	if event.NotificationType == string(notification.NotificationTypeGroupMembership) {
		// Control-plane membership change: group is supplied directly by the backend.
		notif, err = notification.NewGroupMembershipNotification(nodeID, event.GroupID, event.SubGroupIDs, event.Action)
	} else {
		notif, err = notification.NewNotificationFromEvent(nodeID, event.TopicName, event.NotificationType, event.CurrState, event.PrevState, event.Notify)
	}
	if err != nil {
		// Drop rather than return: an unroutable name cannot succeed on retry, and
		// returning err fails the invocation and has the rules engine retry it.
		if errors.Is(err, notification.ErrNoGroupInName) {
			rlog.Info(ctx).Msgf("Dropping notification for node %s: topic '%s' carries no group ID", nodeID, event.TopicName)
			return nil
		}
		rlog.Error(ctx).Err(err).Msgf("Failed to create notification: %s", err)
		return err
	}

	// Create system context for database operations
	s := utils.NewSystemActor()
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, s)
	registry := notification.Registry()

	// A shadow event whose notify.version did not move is a connectivity-only
	// fire (shadow_notify_rule also triggers on reported.online transitions)
	// and still carries the node's lingering notify map. Only services that
	// opt in to connectivity changes receive it; dispatching the rest would
	// re-deliver their last notification on every online flip.
	connectivityOnly := event.NotificationType == string(notification.NotificationTypeShadowUpdate) &&
		!notifyVersionChanged(event.CurrState, event.PrevState)
	if connectivityOnly && !hasConnectivityDispatchTarget(registry, event.Notify) {
		rlog.Debug(rmngCtx).Msgf("Connectivity-only update for node %s with no connectivity-aware service; nothing to dispatch", nodeID)
		return nil
	}

	// Validate node-group alignment for device-originated notifications before
	// processing. This is critical because group info is embedded in both shadow
	// names and notify topic names and devices could specify incorrect names.
	// group_membership_change is exempt: it originates from the trusted backend,
	// and for a "removed" action the node legitimately no longer belongs to the
	// group, so the alignment check would wrongly drop it.
	if notif.NotificationType != notification.NotificationTypeGroupMembership {
		groupID, subGroupIDs, _, err := notif.GetGroupInfo()
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Msgf("Failed to extract group information for validation")
			return err
		}

		err = validateNodeGroupAlignment(rmngCtx, nodeID, groupID, subGroupIDs)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Msgf("Node-group validation failed for node %s", nodeID)
			// Don't fail the entire pipeline, but skip processing for this notification
			// This allows graceful handling of group mismatches while maintaining security
			return nil
		}
		rlog.Info(rmngCtx).Msgf("Node-group validation passed for node %s in group %s with subgroups %v", nodeID, groupID, subGroupIDs)
	}

	// Optimisation: The recipient list depends only on the group, so it is identical for every
	// user-specific service here: a node with alexa + gva + push would otherwise
	// run the same ListUsersForGroupOrSubGroup query three times per event.
	var (
		resolvedUserIDs  []string
		resolvedUsersErr error
		usersResolved    bool
	)
	resolveUsers := func() ([]string, error) {
		if !usersResolved {
			resolvedUserIDs, resolvedUsersErr = getUsersForNotification(rmngCtx, notif)
			usersResolved = true
		}
		return resolvedUserIDs, resolvedUsersErr
	}

	// For keys in event.Notify, we need to get the notification service and send the notification
	for serviceName := range event.Notify {
		if serviceName == "version" {
			// Version is a special case, it is used to track the version of the notification
			continue
		}
		service, err := registry.Get(serviceName)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Msgf("Failed to get notification service: %s", serviceName)
			continue
		}

		if connectivityOnly {
			if cn, ok := service.(notification.ConnectivityNotifier); !ok || !cn.NotifyOnConnectivityChange() {
				rlog.Info(rmngCtx).Msgf("Skipping service %s for connectivity-only update", serviceName)
				continue
			}
		}

		notifData, err := service.Marshal(notif)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Msgf("Failed to marshal notification: %s", serviceName)
			continue
		}

		if service.GetType() == notification.NotificationServiceTypeUserSpecific {
			userIDs, err := resolveUsers()
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msgf("Failed to get users for notification: %s", serviceName)
				continue
			}
			if len(userIDs) == 0 {
				rlog.Info(rmngCtx).Msgf("No users found for notification: %s, ignoring", serviceName)
				continue
			}
			rlog.Info(rmngCtx).Msgf("Sending notification to %d users for service %s", len(userIDs), serviceName)
			err = service.SendTo(notifData, userIDs)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msgf("Failed to send notification to users: %s", serviceName)
				continue
			}
		} else {
			rlog.Info(rmngCtx).Msgf("Sending notification for service %s", serviceName)
			err = service.Send(notifData)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msgf("Failed to send notification: %s", serviceName)
				continue
			}
		}
	}

	return nil
}

func main() {
	initialize()
	lambda.Start(Handler)
}
