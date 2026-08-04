// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// tenant builds a user with one group holding one node.
func tenant(t *testing.T, userID, groupName, nodeID string) (*rmngctx.RmngContext, string) {
	t.Helper()

	ctx := rmngctx.NewRmngContext(user.NewUser(userID))
	grp, err := group.CreateGroupForUser(ctx, groupName)
	if err != nil {
		t.Fatalf("failed to create group for %s: %v", userID, err)
	}
	if err := ctx.SetAllow(utils.NodeAll, nodeID); err != nil {
		t.Fatalf("failed to grant node access for %s: %v", userID, err)
	}
	if _, err := group.AddNode(ctx, grp.GroupID, nodeID, nil); err != nil {
		t.Fatalf("failed to add node for %s: %v", userID, err)
	}
	return ctx, grp.GroupID
}

// authorizeNodeForUser must deny a group the caller has no mapping for. A non-member
// gets an empty list rather than an error, so checking only err would fail open.
func TestAuthorizeNodeForUserRejectsForeignGroup(t *testing.T) {
	test_utils.TestSetup()

	ctxA, _ := tenant(t, "user-a", "Group A", "node-a")
	_, groupB := tenant(t, "user-b", "Group B", "node-b")

	if err := authorizeNodeForUser(ctxA, groupB, "node-b"); err == nil {
		t.Fatal("expected denial for a group the caller has no access to, got nil")
	}
}

// Group access alone must not authorize a node outside that group.
func TestAuthorizeNodeForUserRejectsForeignNodeInOwnGroup(t *testing.T) {
	test_utils.TestSetup()

	ctxA, groupA := tenant(t, "user-a", "Group A", "node-a")
	tenant(t, "user-b", "Group B", "node-b")

	if err := authorizeNodeForUser(ctxA, groupA, "node-b"); err == nil {
		t.Fatal("expected denial for a node that is not a member of the caller's group, got nil")
	}
}

func TestAuthorizeNodeForUserAllowsOwnNode(t *testing.T) {
	test_utils.TestSetup()

	ctxA, groupA := tenant(t, "user-a", "Group A", "node-a")

	if err := authorizeNodeForUser(ctxA, groupA, "node-a"); err != nil {
		t.Fatalf("expected the caller's own node to be authorized, got %v", err)
	}
}
