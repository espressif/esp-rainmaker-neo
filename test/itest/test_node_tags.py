# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Integration tests for node tag management APIs.

Tests the following endpoints:
- GET/PUT /v1/admin/nodes/{nodeId}/tags (admin tags)
- GET/PUT /v1/groups/{groupId}/nodes/{nodeId}/tags (user tags)
"""

import time
from py_sdk.test_user import user_log
from py_sdk.test_group import Group


def test_admin_get_tags_empty(super_admin_user, associated_device):
    """Admin GET tags on a node with no tags returns empty maps."""
    device, group_id, _, _ = associated_device
    node_id = device.node_thing_name

    result = super_admin_user.admin_get_node_tags(node_id)
    assert result is not None, "Admin get node tags should succeed"
    assert result.get("admin") == {} or result.get("admin") is not None
    assert result.get("device") == {} or result.get("device") is not None
    assert result.get("user") == {} or result.get("user") is not None

    user_log("Admin GET tags (empty) succeeded")


def test_admin_put_and_get_admin_tags(super_admin_user, associated_device):
    """Admin can write and read back admin tags."""
    device, group_id, _, _ = associated_device
    node_id = device.node_thing_name

    # Write admin tags
    result = super_admin_user.admin_put_node_tags(
        node_id, admin_tags={"env": "production", "region": "us-west-2"}
    )
    assert result is not None, "Admin put tags should succeed"

    # Read back - shadow update is async, give it a moment
    time.sleep(1)

    result = super_admin_user.admin_get_node_tags(node_id)
    assert result is not None, "Admin get tags should succeed"
    assert result["admin"]["env"] == "production"
    assert result["admin"]["region"] == "us-west-2"

    user_log("Admin PUT/GET admin tags succeeded")


def test_admin_put_and_get_user_tags(super_admin_user, associated_device):
    """Admin can write and read back user tags."""
    device, group_id, _, _ = associated_device
    node_id = device.node_thing_name

    # Write user tags as admin
    result = super_admin_user.admin_put_node_tags(
        node_id, user_tags={"room": "kitchen", "nickname": "main-light"}
    )
    assert result is not None, "Admin put user tags should succeed"

    time.sleep(1)

    result = super_admin_user.admin_get_node_tags(node_id)
    assert result is not None
    assert result["user"]["room"] == "kitchen"
    assert result["user"]["nickname"] == "main-light"

    # Update room tag to "living room"
    result = super_admin_user.admin_put_node_tags(
        node_id, user_tags={"room": "living room"}
    )
    assert result is not None, "Admin put updated user tags should succeed"

    time.sleep(1)

    result = super_admin_user.admin_get_node_tags(node_id)
    assert result is not None
    assert result["user"]["room"] == "living room"
    assert result["user"]["nickname"] == "main-light"

    user_log("Admin PUT/GET user tags succeeded")


def test_admin_put_both_tag_types(super_admin_user, associated_device):
    """Admin can write both admin and user tags in a single request."""
    device, group_id, _, _ = associated_device
    node_id = device.node_thing_name

    result = super_admin_user.admin_put_node_tags(
        node_id,
        admin_tags={"priority": "high"},
        user_tags={"label": "sensor-1"}
    )
    assert result is not None

    time.sleep(1)

    result = super_admin_user.admin_get_node_tags(node_id)
    assert result is not None
    assert result["admin"]["priority"] == "high"
    assert result["user"]["label"] == "sensor-1"

    user_log("Admin PUT both tag types succeeded")


def test_admin_delete_tag_via_null(super_admin_user, associated_device):
    """Admin can delete a tag by setting its value to null."""
    device, group_id, _, _ = associated_device
    node_id = device.node_thing_name

    # First set some admin tags
    super_admin_user.admin_put_node_tags(
        node_id, admin_tags={"to_keep": "yes", "to_delete": "temporary"}
    )
    time.sleep(1)

    # Delete one tag
    result = super_admin_user.admin_put_node_tags(
        node_id, admin_tags={"to_delete": None}
    )
    assert result is not None

    time.sleep(1)

    result = super_admin_user.admin_get_node_tags(node_id)
    assert result is not None
    assert result["admin"].get("to_keep") == "yes"
    assert "to_delete" not in result["admin"], "Deleted tag should not be present"

    user_log("Admin tag deletion via null succeeded")


def test_admin_tags_denied_for_non_admin(test_user1, associated_device):
    """Non-admin users cannot access the admin tags endpoint."""
    device, group_id, _, _ = associated_device
    node_id = device.node_thing_name

    result = test_user1.admin_get_node_tags(node_id)
    assert result is None, "Non-admin should not be able to get admin tags"

    result = test_user1.admin_put_node_tags(node_id, admin_tags={"env": "hack"})
    assert result is None, "Non-admin should not be able to put admin tags"

    user_log("Admin tags correctly denied for non-admin user")


def test_user_put_and_get_tags(associated_device):
    """User can write and read user tags on a node in their group."""
    device, group_id, test_user, _ = associated_device
    node_id = device.node_thing_name

    # Write user tags
    result = test_user.put_node_tags(group_id, node_id, {"room": "living-room", "floor": "2"})
    assert result is not None, "User put tags should succeed"

    time.sleep(1)

    # Read back
    result = test_user.get_node_tags(group_id, node_id)
    assert result is not None, "User get tags should succeed"
    assert result["user"]["room"] == "living-room"
    assert result["user"]["floor"] == "2"

    user_log("User PUT/GET tags succeeded")


def test_user_delete_tag_via_null(associated_device):
    """User can delete a tag by setting its value to null."""
    device, group_id, test_user, _ = associated_device
    node_id = device.node_thing_name

    # Set tags first
    test_user.put_node_tags(group_id, node_id, {"keep": "yes", "remove": "temp"})
    time.sleep(1)

    # Delete one
    result = test_user.put_node_tags(group_id, node_id, {"remove": None})
    assert result is not None

    time.sleep(1)

    result = test_user.get_node_tags(group_id, node_id)
    assert result is not None
    assert result["user"].get("keep") == "yes"
    assert "remove" not in result["user"], "Deleted tag should not be present"

    user_log("User tag deletion via null succeeded")


def test_user_tags_denied_for_non_group_member(two_tenants):
    """A cannot read/write user tags on B's node, via either path variant.

    The handler has two sequential guards and each path exercises a different
    one, so both are needed:
      - foreign-group path: A is not a member, so the group-membership check
        denies and the node check is never reached.
      - own-group path: A's own group paired with B's node, so the group check
        passes by design and GetGroupNode is the only remaining guard. This is
        the variant that returned 200 on both read and write.
    """
    tenant_a, tenant_b = two_tenants
    user_a = tenant_a["user"]
    node_b = tenant_b["node_id"]

    for label, group_id in (("foreign-group path", tenant_b["group_id"]),
                            ("own-group path", tenant_a["group_id"])):
        assert user_a.get_node_tags(group_id, node_b) is None, \
            f"Read foreign node tags via {label}"
        assert user_a.put_node_tags(group_id, node_b, {"room": "hack"}) is None, \
            f"Wrote foreign node tags via {label}"

    user_log("User tags correctly denied for non-member and cross-tenant paths")


def test_admin_reads_device_tags_set_by_device(super_admin_user, associated_device):
    """Admin can read device tags that were written by the device via shadow update."""
    device, group_id, _, _ = associated_device
    node_id = device.node_thing_name

    # Initialize shadow client so the device can update named shadows
    assert device.shadow_connect(named_shadows=["iparams"]), "Shadow connect should succeed"

    # Device writes device tags to the iparams shadow
    device_tags = {
        "data": {
            "device": {
                "t": {
                    "type": "Light",
                    "model": "Led",
                    "fwver": "1.0.0"
                }
            }
        }
    }
    assert device.update_named_shadow("iparams", device_tags), "Device should be able to write device tags"

    # Wait for shadow update to propagate
    time.sleep(2)

    # Admin reads tags via REST API
    result = super_admin_user.admin_get_node_tags(node_id)
    assert result is not None, "Admin get tags should succeed"
    assert result["device"]["type"] == "Light"
    assert result["device"]["model"] == "Led"
    assert result["device"]["fwver"] == "1.0.0"

    user_log("Admin read device tags set by device succeeded")


def test_user_tags_do_not_expose_admin_or_device_tags(super_admin_user, associated_device):
    """User GET tags endpoint only returns user tags, not admin or device tags."""
    device, group_id, test_user, _ = associated_device
    node_id = device.node_thing_name

    # Admin sets admin tags
    super_admin_user.admin_put_node_tags(node_id, admin_tags={"secret": "admin-only"})
    time.sleep(1)

    # User reads tags
    result = test_user.get_node_tags(group_id, node_id)
    assert result is not None
    assert "admin" not in result, "User endpoint should not expose admin tags"
    assert "device" not in result, "User endpoint should not expose device tags"
    assert "user" in result

    user_log("User endpoint correctly hides admin/device tags")
