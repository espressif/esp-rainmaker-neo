// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
)

// injectAlexaScopeToken must place the per-endpoint bearer token where each
// report type expects it: on the endpoint scope for a ChangeReport, and in the
// payload scope for the Alexa.Discovery AddOrUpdate/DeleteReport.
func TestInjectAlexaScopeToken(t *testing.T) {
	const token = "tok-123"

	t.Run("change report -> endpoint scope", func(t *testing.T) {
		report, _ := createEmptyChangeReport()
		injectAlexaScopeToken(&report, token)
		if report.Event.Endpoint == nil || report.Event.Endpoint.Scope == nil ||
			report.Event.Endpoint.Scope.Token != token {
			t.Fatalf("expected token on endpoint scope, got %+v", report.Event.Endpoint)
		}
	})

	t.Run("add-or-update report -> payload scope", func(t *testing.T) {
		report := newDiscoveryReport("AddOrUpdateReport", &AddOrUpdateReportPayload{
			Endpoints: []DiscoveryEndpoint{{EndpointID: "node123_dev0"}},
		})
		injectAlexaScopeToken(&report, token)
		p := (*report.Event.Payload).(*AddOrUpdateReportPayload)
		if p.Scope == nil || p.Scope.Token != token || p.Scope.Type != "BearerToken" {
			t.Fatalf("expected token in payload scope, got %+v", p.Scope)
		}
	})

	t.Run("delete report -> payload scope", func(t *testing.T) {
		report := newDiscoveryReport("DeleteReport", &DeleteReportPayload{
			Endpoints: []DeleteReportEndpoint{{EndpointID: "node123_dev0"}},
		})
		injectAlexaScopeToken(&report, token)
		p := (*report.Event.Payload).(*DeleteReportPayload)
		if p.Scope == nil || p.Scope.Token != token {
			t.Fatalf("expected token in payload scope, got %+v", p.Scope)
		}
	})
}

func TestMarshalGroupMembershipNegative(t *testing.T) {
	a := &AlexaNotification{}

	t.Run("nil membership data", func(t *testing.T) {
		notif := &notification.Notification{NotificationType: notification.NotificationTypeGroupMembership}
		if _, err := a.marshalGroupMembership(notif); err == nil {
			t.Fatal("expected error for nil group membership data, got nil")
		}
	})

	t.Run("unsupported action", func(t *testing.T) {
		notif := &notification.Notification{
			NotificationType:    notification.NotificationTypeGroupMembership,
			GroupID:             "grpABC",
			GroupMembershipData: &notification.GroupMembershipNotification{NodeID: "node123", Action: "moved"},
		}
		if _, err := a.marshalGroupMembership(notif); err == nil {
			t.Fatal("expected error for unsupported action, got nil")
		}
	})
}
