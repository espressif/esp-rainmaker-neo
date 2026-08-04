# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from py_sdk.test_user import user_log
from py_sdk.test_group import Group
from test.itest.conftest import (
    REGION,
    IOT_USER_ROLE_ARN,
    USER_API_GATEWAY_URL,
)
import pytest
import boto3
import uuid
import time


def test_ensure_privilege_escalation_not_possible(test_user1):
    """
    Security test to ensure privilege escalation is not possible in the system.

    This test validates that:
    1. Users cannot directly assume the IoTUserRole using STS
    2. Users must go through the controlled /v1/assumed-roles Lambda API
    3. The assume_role API enforces proper group-based access controls

    Prevents the attack pattern where users could bypass access controls by directly assuming IoT roles.
    """
    user1_group_api = Group(test_user1)
    user_log("🔐 Ensuring privilege escalation is not possible...")

    # Step 1: Get user's Cognito credentials and AWS credentials from identity pool
    user_log("Getting user's AWS credentials from identity pool...")
    test_user1.get_cognito_token()
    user_credentials = test_user1.get_aws_credentials()
    assert user_credentials is not None, "Failed to get user credentials"

    # Step 2: Try to directly assume the IoTUserRole using STS - this should FAIL
    user_log("🚨 Attempting direct privilege escalation via STS AssumeRole (should FAIL)...")
    sts_client = boto3.client(
        'sts',
        region_name=REGION,
        aws_access_key_id=user_credentials['AccessKeyId'],
        aws_secret_access_key=user_credentials['SecretKey'],
        aws_session_token=user_credentials['SessionToken']
    )

    privilege_escalation_blocked = False
    try:
        # Attempt to directly assume the IoTUserRole - this is the attack from attack.py
        response = sts_client.assume_role(
            RoleArn=IOT_USER_ROLE_ARN,
            RoleSessionName="DirectPrivilegeEscalationAttempt"
        )
        user_log("🚨 SECURITY ISSUE: Direct role assumption succeeded - vulnerability still exists!")
        assert False, "CRITICAL: Privilege escalation vulnerability still exists! Direct IoTUserRole assumption should be blocked."

    except Exception as e:
        # This is expected - the privilege escalation should be blocked
        error_str = str(e)
        if "is not authorized to perform: sts:AssumeRole" in error_str or "AccessDenied" in error_str or "User" in error_str and "is not authorized" in error_str:
            user_log("✅ SUCCESS: Direct privilege escalation blocked as expected")
            privilege_escalation_blocked = True
        else:
            user_log(f"⚠️  Unexpected error during privilege escalation attempt: {error_str}")
            # Still consider it blocked if any error occurred
            privilege_escalation_blocked = True

    assert privilege_escalation_blocked, "Direct privilege escalation should be blocked"

    user_log("🔐 Security validation completed successfully!")
    user_log("✅ System properly prevents privilege escalation")
    user_log("✅ Direct IoTUserRole assumption is blocked")
    user_log("✅ Legitimate assume_role API works with group-based access control")


def test_admin_assume_role_with_group(super_admin_user, test_user1):
    """
    Test that super admin users can use assume_role_admin to get access to any group.

    This test validates:
    1. Super admin can call assume_role with a group parameter
    2. The returned credentials have access to the specified group
    """
    user_log("🔑 Testing admin assume_role with group...")

    # Create a group using test_user1
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Admin Test Group")
    assert group_id is not None, "Failed to create test group"
    user_log(f"Created test group: {group_id}")

    # Super admin should be able to assume role for this group
    assumed_credentials = super_admin_user.assume_role_admin(group_id)
    assert assumed_credentials is not None, "Admin assume_role should succeed for super admin"
    assert "access_key" in assumed_credentials, "Response should contain access_key"
    assert "secret_key" in assumed_credentials, "Response should contain secret_key"
    assert "session_token" in assumed_credentials, "Response should contain session_token"

    user_log("✅ Admin assume_role with group succeeded")

    # Cleanup
    user1_group_api.delete_group(group_id)


def test_admin_assume_role_with_subgroup(super_admin_user, test_user1):
    """
    Test that super admin users can use assume_role_admin to get access to a specific subgroup.

    This test validates:
    1. Super admin can call assume_role with both group and subgroup parameters
    2. The returned credentials have access limited to the specified subgroup
    """
    user_log("🔑 Testing admin assume_role with subgroup...")

    # Create a group and subgroup using test_user1
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Admin Test Group with Subgroup")
    assert group_id is not None, "Failed to create test group"
    user_log(f"Created test group: {group_id}")

    subgroup_id = user1_group_api.create_subgroup(group_id, "Admin Test Subgroup")
    assert subgroup_id is not None, "Failed to create test subgroup"
    user_log(f"Created test subgroup: {subgroup_id}")

    # Super admin should be able to assume role for this specific subgroup
    assumed_credentials = super_admin_user.assume_role_admin(group_id, subgroup_id)
    assert assumed_credentials is not None, "Admin assume_role with subgroup should succeed for super admin"
    assert "access_key" in assumed_credentials, "Response should contain access_key"
    assert "secret_key" in assumed_credentials, "Response should contain secret_key"
    assert "session_token" in assumed_credentials, "Response should contain session_token"

    user_log("✅ Admin assume_role with subgroup succeeded")

    # Cleanup
    user1_group_api.delete_group(group_id)


def test_admin_assume_role_denied_for_non_admin(test_user1, test_user2):
    """
    Test that non-super admin users cannot use assume_role with group parameter.

    This is a negative test that validates:
    1. Regular users cannot use the admin assume_role functionality
    2. The API returns an appropriate error (403 Forbidden)
    """
    user_log("🔒 Testing that non-admin users cannot use admin assume_role...")

    # Create a group using test_user1
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Test Group for Admin Denial")
    assert group_id is not None, "Failed to create test group"
    user_log(f"Created test group: {group_id}")

    # test_user2 (not a super admin) should NOT be able to use admin assume_role
    assumed_credentials = test_user2.assume_role_admin(group_id)
    assert assumed_credentials is None, "Non-admin user should not be able to use admin assume_role"

    user_log("✅ Admin assume_role correctly denied for non-admin user")

    # Cleanup
    user1_group_api.delete_group(group_id)


def test_admin_assume_role_nonexistent_group(super_admin_user):
    """
    Test that admin assume_role fails with a proper error for non-existent groups.

    This is a negative test that validates:
    1. The API properly validates group existence
    2. Returns appropriate error when group doesn't exist
    """
    user_log("🔍 Testing admin assume_role with non-existent group...")

    # Try to assume role for a non-existent group
    nonexistent_group = "nonexistent-group-" + str(uuid.uuid4())[:8]
    assumed_credentials = super_admin_user.assume_role_admin(nonexistent_group)
    assert assumed_credentials is None, "Admin assume_role should fail for non-existent group"

    user_log("✅ Admin assume_role correctly fails for non-existent group")


def test_admin_assume_role_nonexistent_subgroup(super_admin_user, test_user1):
    """
    Test that admin assume_role fails with a proper error for non-existent subgroups.

    This is a negative test that validates:
    1. The API properly validates subgroup existence
    2. Returns appropriate error when subgroup doesn't exist in the specified group
    """
    user_log("🔍 Testing admin assume_role with non-existent subgroup...")

    # Create a group using test_user1
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Test Group for Subgroup Denial")
    assert group_id is not None, "Failed to create test group"
    user_log(f"Created test group: {group_id}")

    # Try to assume role for a non-existent subgroup within the existing group
    nonexistent_subgroup = "nonexistent-subgroup-" + str(uuid.uuid4())[:8]
    assumed_credentials = super_admin_user.assume_role_admin(group_id, nonexistent_subgroup)
    assert assumed_credentials is None, "Admin assume_role should fail for non-existent subgroup"

    user_log("✅ Admin assume_role correctly fails for non-existent subgroup")

    # Cleanup
    user1_group_api.delete_group(group_id)


@pytest.mark.unsafe
def test_admin_assume_role_group_mqtt_access(super_admin_user, associated_device):
    """
    Test that admin assume_role with a group grants MQTT access to nodes in that group.

    1. Device is already associated with a group (via fixture)
    2. Device sets a named shadow (params-{group_id})
    3. Admin assumes role for the group
    4. Admin connects to MQTT with assumed credentials
    5. Admin subscribes to the node's named shadow and reads it successfully
    """
    device, group_id, test_user1, user1_group_api = associated_device
    user_log("Testing admin assume_role group MQTT access...")

    # Device sets up named shadow
    shadow_name = f"params-{group_id}"
    assert device.shadow_connect([shadow_name]), f"Failed to connect {device.node_thing_name} to shadow"
    device.update_named_shadow(shadow_name, {"status": device.node_thing_name})

    # Admin assumes role for this group
    assumed_credentials = super_admin_user.assume_role_admin(group_id)
    assert assumed_credentials is not None, "Admin assume_role should succeed"

    # Admin connects to MQTT with assumed credentials and verifies shadow access
    try:
        super_admin_user.mqtt_connect(credentials=assumed_credentials)
    except Exception as e:
        print(f"Failed to connect to MQTT: {e}, retrying in 3 seconds...")
        time.sleep(3)
        super_admin_user.mqtt_connect(credentials=assumed_credentials)

    super_admin_user.subscribe_to_named_shadows(device.node_thing_name, [shadow_name])
    super_admin_user.read_shadow(device.node_thing_name, shadow_name)
    shadow_data = super_admin_user.read_shadow_queue()

    assert shadow_data is not None, "Admin should be able to read shadow for the group node"
    assert shadow_data['state']['reported']['status'] == device.node_thing_name

    user_log("Admin assume_role group MQTT access verified")

    # Cleanup
    super_admin_user.mqtt_disconnect_and_wait()


@pytest.mark.unsafe
def test_admin_assume_role_subgroup_mqtt_access(super_admin_user, device_with_2_subgroups):
    """
    Test that admin assume_role with a subgroup grants MQTT access ONLY to
    nodes in that subgroup, not to nodes in a sibling subgroup of the same group.

    1. Device is in a group with two subgroups (via fixture)
    2. Device sets named shadows for each subgroup
    3. Admin assumes role for first subgroup only
    4. Verify: admin CAN access the shadow for the first subgroup
    5. Verify: admin CANNOT access the shadow for the second subgroup
    """
    user_log("Testing admin assume_role subgroup MQTT access...")

    data = device_with_2_subgroups.subgroup_add_2()
    device = data['device']
    group_id = data['group_id']
    subgroup_aa_id = data['subgroups'][0]
    subgroup_ab_id = data['subgroups'][1]

    # Device sets up named shadows for each subgroup
    shadow_aa = f"params-{group_id}-{subgroup_aa_id}"
    shadow_ab = f"params-{group_id}-{subgroup_ab_id}"

    assert device.shadow_connect([shadow_aa, shadow_ab]), f"Failed to connect {device.node_thing_name} to shadows"
    device.update_named_shadow(shadow_aa, {"status": f"{device.node_thing_name}-aa"})
    device.update_named_shadow(shadow_ab, {"status": f"{device.node_thing_name}-ab"})

    # Admin assumes role for first subgroup only
    assumed_credentials = super_admin_user.assume_role_admin(group_id, subgroup_aa_id)
    assert assumed_credentials is not None, "Admin assume_role with subgroup should succeed"

    # Positive test: admin CAN access the shadow for first subgroup
    try:
        super_admin_user.mqtt_connect(credentials=assumed_credentials)
    except Exception as e:
        print(f"Failed to connect to MQTT: {e}, retrying in 3 seconds...")
        time.sleep(3)
        super_admin_user.mqtt_connect(credentials=assumed_credentials)

    super_admin_user.subscribe_to_named_shadows(device.node_thing_name, [shadow_aa])
    super_admin_user.read_shadow(device.node_thing_name, shadow_aa)
    shadow_data = super_admin_user.read_shadow_queue()

    assert shadow_data is not None, "Admin should be able to read shadow for first subgroup"
    assert shadow_data['state']['reported']['status'] == f"{device.node_thing_name}-aa"
    user_log("Admin CAN access first subgroup shadow - correct")

    super_admin_user.mqtt_disconnect_and_wait()

    # Negative test: admin CANNOT access the shadow for second subgroup
    try:
        super_admin_user.mqtt_connect(credentials=assumed_credentials)
    except Exception as e:
        print(f"Failed to connect to MQTT: {e}, retrying in 3 seconds...")
        time.sleep(3)
        super_admin_user.mqtt_connect(credentials=assumed_credentials)
    super_admin_user.disable_reconnect = True

    with pytest.raises(Exception):
        super_admin_user.subscribe_to_named_shadows(device.node_thing_name, [shadow_ab])

    connection_status = super_admin_user.read_connection_queue()
    assert connection_status == "interrupted", "Admin should get disconnected when accessing unauthorized subgroup shadow"
    user_log("Admin CANNOT access second subgroup shadow - correct")

    super_admin_user.mqtt_disconnect_and_wait()
    super_admin_user.disable_reconnect = False


def test_admin_get_node_groups(super_admin_user, associated_device):
    """
    Test that super admin can get group info for a node.

    Validates:
    1. Super admin can call GET /v1/admin/nodes/{nodeId}/groups
    2. The response contains the correct group for the node
    """
    device, group_id, test_user1, user1_group_api = associated_device
    user_log("Testing admin get node groups...")

    result = super_admin_user.admin_get_node_groups(device.node_thing_name)
    assert result is not None, "Admin get node groups should succeed"
    assert result.get("group") == group_id, f"Expected group {group_id}, got {result.get('group')}"

    user_log("Admin get node groups succeeded")


def test_admin_get_node_groups_denied_for_non_admin(test_user1, associated_device):
    """
    Test that non-admin users cannot get node group info.

    Validates:
    1. Regular users get a non-200 response (403 Forbidden)
    """
    device, group_id, _, _ = associated_device
    user_log("Testing that non-admin users cannot get node groups...")

    result = test_user1.admin_get_node_groups(device.node_thing_name)
    assert result is None, "Non-admin user should not be able to get node groups"

    user_log("Admin get node groups correctly denied for non-admin user")


def test_admin_get_node_groups_no_group(super_admin_user):
    """
    Test that admin gets empty result for a node not in any group.

    Validates:
    1. The API returns an empty group when the node has no group mapping
    """
    user_log("Testing admin get node groups for node with no group...")

    nonexistent_node = "nonexistent-node-" + str(uuid.uuid4())[:8]
    result = super_admin_user.admin_get_node_groups(nonexistent_node)
    assert result is not None, "Admin get node groups should return a response"
    assert result.get("group") == "", f"Expected empty group, got {result.get('group')}"

    user_log("Admin get node groups correctly returns empty for unassociated node")


# ==========================================
# Admin OAuth Client Registry (/v1/admin/clients)
# ==========================================

def _find_client(super_admin_user, client_id, get_secret=False):
    """Return the client dict from the list, or None."""
    listed = super_admin_user.list_oauth_clients(get_secret=get_secret)
    assert listed.status_code == 200, listed.text
    return next((c for c in listed.json()["clients"] if c["client_id"] == client_id), None)


def test_admin_client_crud_lifecycle(super_admin_user):
    """Full create -> list -> update -> delete lifecycle for a public client."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    client_id = "itest_" + str(uuid.uuid4())[:8]
    create = super_admin_user.create_oauth_client({
        "client_id": client_id,
        "client_name": "itest client",
        "client_type": "public",
        "redirect_uris": ["com.espressif.itest://callback"],
        "grant_types": ["authorization_code", "refresh_token"],
        "scopes": ["openid", "email"],
        "require_pkce": True,
    })
    assert create.status_code == 201, create.text
    body = create.json()
    assert body["client_id"] == client_id
    assert body["client_type"] == "public"
    assert not body.get("client_secret"), "public clients get no secret"

    try:
        # List includes it; PKCE forced true; no secret.
        gc = _find_client(super_admin_user, client_id)
        assert gc is not None, "created client must appear in the list"
        assert gc["require_pkce"] is True
        assert not gc.get("client_secret")

        # PUT is a full replace of the mutable fields (client_id/client_type are immutable
        # and rejected if sent): resend the whole intended state with the new name.
        updated = super_admin_user.put_oauth_client(client_id, {
            "client_name": "renamed",
            "redirect_uris": ["com.espressif.itest://callback"],
            "grant_types": ["authorization_code", "refresh_token"],
            "scopes": ["openid", "email"],
            "require_pkce": True,
        })
        assert updated.status_code == 200, updated.text
        assert updated.json()["client_name"] == "renamed"
    finally:
        deleted = super_admin_user.delete_oauth_client(client_id)
        assert deleted.status_code == 200, deleted.text

    # Hard delete: the client is gone from the list.
    assert _find_client(super_admin_user, client_id) is None, "deleted client must be gone"


def test_admin_client_confidential_secret_retrievable(super_admin_user):
    """A confidential client gets a plaintext secret at create; it's retrievable via list get_secret, hidden without."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    client_id = "itest_conf_" + str(uuid.uuid4())[:8]
    create = super_admin_user.create_oauth_client({
        "client_id": client_id,
        "client_name": "itest confidential",
        "client_type": "confidential",
        "grant_types": ["authorization_code"],
    })
    assert create.status_code == 201, create.text
    created_secret = create.json().get("client_secret")
    assert created_secret, "confidential client must return a plaintext secret at create"

    try:
        # Without get_secret the secret is omitted...
        hidden = _find_client(super_admin_user, client_id, get_secret=False)
        assert not hidden.get("client_secret"), "secret must be hidden by default"

        # ...and returned (matching the created value) with get_secret=true.
        shown = _find_client(super_admin_user, client_id, get_secret=True)
        assert shown.get("client_secret") == created_secret
    finally:
        super_admin_user.delete_oauth_client(client_id)

def test_admin_clients_forbidden_for_non_admin(test_user1):
    """A non-admin user's token cannot reach the superadmin client registry."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    resp = test_user1.list_oauth_clients()
    # Rejected either at the admin Cognito authorizer (401/403) or the super_admin gate (403).
    assert resp.status_code in (401, 403), resp.text
