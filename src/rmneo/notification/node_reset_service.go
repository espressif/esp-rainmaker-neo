// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// NodeResetService handles the node_reset direct notification: a node that
// has factory-reset itself announces it is no longer valid. The backend
// disassociates the node from its group and runs the async data cleanup
// (triggers, schedules, timeseries, automations).
//
// Like AutomationService, this is a server-side action rather than a user
// notification: it is Generic (not user-specific), runs entirely in Send,
// and ignores the user list.
//
// TODO: also notify group members so their apps can evict the node from
// cache (event_data.type == "node_reset"). Deferred until the mobile app
// handles that event; re-introduce as a separate user-specific push step.
type NodeResetService struct{}

// NewNodeResetService creates a new NodeResetService.
func NewNodeResetService() *NodeResetService {
	return &NodeResetService{}
}

// GetName returns the notify-map key this service handles.
func (s *NodeResetService) GetName() string {
	return "node_reset"
}

// GetType marks this as generic: node_reset is a server-side action, not a
// per-user notification, so the handler calls Send (not SendTo) and never
// resolves group members.
func (s *NodeResetService) GetType() NotificationServiceType {
	return NotificationServiceTypeGeneric
}

// Send disassociates the reset node from its group and triggers the async
// data cleanup. node_reset is only valid as a direct notification; the node
// ID comes from DirectNotificationData (NOT ShadowUpdateData, which is nil
// for direct notifications).
func (s *NodeResetService) Send(notificationData interface{}) error {
	notif, ok := notificationData.(*Notification)
	if !ok {
		rlog.Error(context.TODO()).Msg("Failed to cast notification to Notification")
		return nil
	}

	// Build a system context locally — the notifications Lambda already runs
	// as a privileged system identity (same pattern as AutomationService).
	s_actor := utils.NewSystemActor()
	ctx := rmngctx.NewRmngContextWithCtx(context.Background(), s_actor)

	if notif.NotificationType != NotificationTypeDirect || notif.DirectNotificationData == nil {
		rlog.Debug(ctx).Msgf("node_reset ignoring notification type: %s", notif.NotificationType)
		return nil
	}

	nodeID := notif.DirectNotificationData.NodeID
	if nodeID == "" {
		rlog.Error(ctx).Msg("node_reset requires a node ID")
		return nil
	}

	// Group information is parsed and validated by the main handler before dispatch.
	groupID, _, _, err := notif.GetGroupInfo()
	if err != nil {
		return err
	}

	if err := node.ShadowNodeRemoveFromGroupAuthorized(ctx, nodeID, groupID); err != nil {
		return rmerror.NewRMError(err, "Failed to remove reset node from group")
	}

	rlog.Info(ctx).Msgf("Successfully processed node_reset for node %s in group %s", nodeID, groupID)
	return nil
}

// SendTo is unused for a generic service; node_reset is not user-specific.
func (s *NodeResetService) SendTo(notificationData interface{}, userIDs []string) error {
	return s.Send(notificationData)
}

// Marshal passes the notification through unchanged; Send reads it directly
// (same pattern as AutomationService).
func (s *NodeResetService) Marshal(notif *Notification) (interface{}, error) {
	return notif, nil
}
