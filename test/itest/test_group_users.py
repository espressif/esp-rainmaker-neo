# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import uuid
import pytest
from test.itest.conftest import accept_sharing_request_for
from py_sdk.test_group import Group


def test_list_group_users_owner_only(test_user1):
    """Owner creates a group and lists users — should see only themselves."""
    group_api = Group(test_user1)
    group_id = group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        data = group_api.list_group_users(group_id)
        assert data["users"] == [
            {"user_id": test_user1.sub, "email": test_user1.username, "access_type": "primary"},
        ]
    finally:
        group_api.delete_group(group_id, warn_error=True)


def test_list_group_users_after_sharing(test_user1, test_user2):
    """Share a group with secondary access, then verify both users appear."""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        user1_group_api.share_group(group_id, test_user2.username, "secondary")
        accept_sharing_request_for(test_user2, group_id, "")

        data = user1_group_api.list_group_users(group_id)
        assert sorted(data["users"], key=lambda u: u["user_id"]) == sorted([
            {"user_id": test_user1.sub, "email": test_user1.username, "access_type": "primary"},
            {"user_id": test_user2.sub, "email": test_user2.username, "access_type": "secondary"},
        ], key=lambda u: u["user_id"])
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


def test_list_group_users_secondary_sees_primaries_only(test_user1, test_user2):
    """A secondary user may list group users, but the response is narrowed to primary
    owners — the secondary caller does not see itself or other secondaries."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        user1_group_api.share_group(group_id, test_user2.username, "secondary")
        accept_sharing_request_for(test_user2, group_id, "")

        data = user2_group_api.list_group_users(group_id)
        assert data["users"] == [
            {"user_id": test_user1.sub, "email": test_user1.username, "access_type": "primary"},
        ]
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


def test_list_group_users_unauthorized(test_user1, test_user2):
    """A user with no access to the group should get an error."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        response = user2_group_api.list_group_users_raw(group_id)
        assert response.status_code != 200, "Expected non-200 for unauthorized user"
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


def test_list_users_then_unshare(test_user1, test_user2):
    """Full flow: list users to discover user_id, then unshare that user."""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        # Share
        user1_group_api.share_group(group_id, test_user2.username, "secondary")
        accept_sharing_request_for(test_user2, group_id, "")

        # List users and find the non-owner
        data = user1_group_api.list_group_users(group_id)
        target_user_id = next(u["user_id"] for u in data["users"] if u["user_id"] != test_user1.sub)

        # Unshare using the discovered user_id
        user1_group_api.unshare_group(group_id, target_user_id)

        # Verify only owner remains
        data = user1_group_api.list_group_users(group_id)
        assert data["users"] == [
            {"user_id": test_user1.sub, "email": test_user1.username, "access_type": "primary"},
        ]
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


def _subgroup_exists(group_api, group_id, subgroup_id):
    """True if subgroup_id is still listed under group_id for this user."""
    group = group_api._get_group(group_id)
    return group is not None and any(s["subgroup_id"] == subgroup_id for s in group.get("subgroups", []))


def test_primary_user_can_delete_subgroup(test_user1):
    """The primary owner can delete a subgroup in their own group."""
    group_api = Group(test_user1)
    group_id = group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        subgroup_id = group_api.create_subgroup(group_id, "Subgroup To Delete")
        group_api.delete_subgroup(group_id, subgroup_id)
        assert not _subgroup_exists(group_api, group_id, subgroup_id)
    finally:
        group_api.delete_group(group_id, warn_error=True)


def test_secondary_user_can_delete_subgroup(test_user1, test_user2):
    """A secondary user (shared the whole group) can delete a subgroup."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        subgroup_id = user1_group_api.create_subgroup(group_id, "Subgroup To Delete")
        user1_group_api.share_group(group_id, test_user2.username, "secondary")
        accept_sharing_request_for(test_user2, group_id, "")

        user2_group_api.delete_subgroup(group_id, subgroup_id)
        assert not _subgroup_exists(user1_group_api, group_id, subgroup_id)
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


def test_list_group_users_subgroup_shows_subgroups(test_user1, test_user2):
    """Sharing a subgroup should show the user with subentity access and subgroups."""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        subgroup_id = user1_group_api.create_subgroup(group_id, "Test Subgroup")
        user1_group_api.share_subgroup(group_id, subgroup_id, test_user2.username)
        accept_sharing_request_for(test_user2, group_id, subgroup_id)

        data = user1_group_api.list_group_users(group_id)
        assert sorted(data["users"], key=lambda u: u["user_id"]) == sorted([
            {"user_id": test_user1.sub, "email": test_user1.username, "access_type": "primary"},
            {"user_id": test_user2.sub, "email": test_user2.username, "access_type": "subgroup", "subgroups": [subgroup_id]},
        ], key=lambda u: u["user_id"])
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


def test_list_subgroup_users_owner_only(test_user1):
    """Owner creates a subgroup and lists its users — should see only themselves,
    scoped to the queried subgroup."""
    group_api = Group(test_user1)
    group_id = group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        subgroup_id = group_api.create_subgroup(group_id, "Test Subgroup")

        data = group_api.list_subgroup_users(group_id, subgroup_id)
        assert data["users"] == [
            {"user_id": test_user1.sub, "email": test_user1.username, "access_type": "primary"},
        ]
    finally:
        group_api.delete_group(group_id, warn_error=True)


def test_list_subgroup_users_scoped_by_caller_access(test_user1, test_user2):
    """The listing scope is determined by the caller's access level: a primary caller sees the
    full membership, while a subgroup-only (subentity) member sees just the primary owners."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        subgroup_id = user1_group_api.create_subgroup(group_id, "Test Subgroup")
        user1_group_api.share_subgroup(group_id, subgroup_id, test_user2.username)
        accept_sharing_request_for(test_user2, group_id, subgroup_id)

        # Primary caller sees the full membership.
        data = user1_group_api.list_subgroup_users(group_id, subgroup_id)
        assert sorted(data["users"], key=lambda u: u["user_id"]) == sorted([
            {"user_id": test_user1.sub, "email": test_user1.username, "access_type": "primary"},
            {"user_id": test_user2.sub, "email": test_user2.username, "access_type": "subgroup", "subgroups": [subgroup_id]},
        ], key=lambda u: u["user_id"])

        # Subgroup-only member sees just the primary owners.
        data = user2_group_api.list_subgroup_users(group_id, subgroup_id)
        assert data["users"] == [
            {"user_id": test_user1.sub, "email": test_user1.username, "access_type": "primary"},
        ]
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


def test_list_subgroup_users_secondary_member_defaults_to_primary(test_user1, test_user2):
    """A group-level secondary member (not primary) also defaults to the primary-owners
    listing, not the full membership."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        subgroup_id = user1_group_api.create_subgroup(group_id, "Test Subgroup")
        user1_group_api.share_group(group_id, test_user2.username, "secondary")
        accept_sharing_request_for(test_user2, group_id, "")

        data = user2_group_api.list_subgroup_users(group_id, subgroup_id)
        assert data["users"] == [
            {"user_id": test_user1.sub, "email": test_user1.username, "access_type": "primary"},
        ]
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)
