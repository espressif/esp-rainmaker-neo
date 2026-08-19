// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
)

// newGVA builds a mock-mode GVA adapter (no SSM / Google calls).
func newGVATestNotification() *GVANotification {
	return &GVANotification{isTestMode: true, reportURI: "https://example.test/gva/data"}
}

func groupMembershipNotif(t *testing.T, action string) *notification.Notification {
	t.Helper()
	notif, err := notification.NewGroupMembershipNotification("node123", "grpABC", nil, action)
	if err != nil {
		t.Fatalf("failed to build notification: %v", err)
	}
	return notif
}

func TestGVAMarshalGroupMembershipReturnsRequestSync(t *testing.T) {
	g := newGVATestNotification()

	for _, action := range []string{notification.GroupMembershipActionAdded, notification.GroupMembershipActionRemoved} {
		out, err := g.Marshal(groupMembershipNotif(t, action))
		if err != nil {
			t.Fatalf("action %q: unexpected error: %v", action, err)
		}
		reqs, ok := out.([]GVARequestSyncRequest)
		if !ok {
			t.Fatalf("action %q: expected []GVARequestSyncRequest, got %T", action, out)
		}
		if len(reqs) != 1 || reqs[0].Async {
			t.Fatalf("action %q: expected a single synchronous request sync, got %+v", action, reqs)
		}
	}
}

func TestGVAMarshalGroupMembershipNilDataErrors(t *testing.T) {
	g := newGVATestNotification()
	// A group_membership notification with no data must be rejected, not panic.
	notif := &notification.Notification{NotificationType: notification.NotificationTypeGroupMembership}
	if _, err := g.Marshal(notif); err == nil {
		t.Fatal("expected error for nil group membership data, got nil")
	}
}

func TestRequestSyncEndpoint(t *testing.T) {
	const want = "https://homegraph.googleapis.com/v1/devices:requestSync"
	if RequestSyncEndpoint != want {
		t.Errorf("RequestSyncEndpoint = %q, want %q", RequestSyncEndpoint, want)
	}
}
