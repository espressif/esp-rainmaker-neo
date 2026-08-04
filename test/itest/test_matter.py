# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from py_sdk.test_group import Group
from py_sdk.test_matter import (
    build_nocsr_elements_tlv,
    sign_attestation_data,
    do_initiate,
    do_verify_with_nocsr_elements,
    do_confirm,
    do_matter_dev_assoc,
)
from test.itest.conftest import (
    accept_sharing_request_for,
    extract_matter_oids,
    verify_certificate_signed_by,
    connect_device_with_retry,
)
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives import hashes, serialization
from cryptography import x509
import pytest
import json
import uuid
import os


def test_create_matter_group(test_user1):
    """Test creating a group with Matter capability returns all required fields."""
    user1_group_api = Group(test_user1)
    group_id = None

    try:
        # Create group with Matter capability
        response_data = user1_group_api.create_matter_group("Test Matter Group")
        group_id = response_data["group_id"]
        matter = response_data["matter"]

        # Validate Matter fields
        assert "fabric_id" in matter, "Matter object should contain fabric_id"
        assert len(matter["fabric_id"]) == 16, f"fabric_id should be 16 hex chars, got {len(matter['fabric_id'])}"
        assert all(c in '0123456789abcdefABCDEF' for c in matter["fabric_id"]), "fabric_id should be hex string"

        assert "root_ca" in matter, "Matter object should contain root_ca"
        assert matter["root_ca"].startswith("-----BEGIN CERTIFICATE-----"), "root_ca should be valid PEM certificate"

        assert "ipk" in matter, "Matter object should contain ipk"
        assert len(matter["ipk"]) == 32, f"ipk should be 32 hex chars, got {len(matter['ipk'])}"
        assert all(c in '0123456789abcdefABCDEF' for c in matter["ipk"]), "ipk should be hex string"

        assert "group_cat_id_admin" in matter, "Matter object should contain group_cat_id_admin"
        assert len(matter["group_cat_id_admin"]) == 8, f"group_cat_id_admin should be 8 hex chars, got {len(matter['group_cat_id_admin'])}"

        assert "group_cat_id_operate" in matter, "Matter object should contain group_cat_id_operate"
        assert len(matter["group_cat_id_operate"]) == 8, f"group_cat_id_operate should be 8 hex chars, got {len(matter['group_cat_id_operate'])}"

        # Verify Root CA contains Matter OIDs
        root_ca_oids = extract_matter_oids(matter["root_ca"])
        assert root_ca_oids["fabric_id"] is not None, "Root CA should contain Fabric ID OID"
        assert root_ca_oids["rcac_id"] is not None, "Root CA should contain RCAC ID OID"

    finally:
        if group_id:
            user1_group_api.delete_group(group_id)


def test_matter_user_noc(test_user1):
    """Test User NOC generation and validation for Matter groups."""
    user1_group_api = Group(test_user1)
    group_id = None

    try:
        # Create Matter group
        response_data = user1_group_api.create_matter_group("Test Matter NOC Group")
        group_id = response_data["group_id"]
        matter = response_data["matter"]
        root_ca = matter["root_ca"]

        # Request User NOC (CSR is auto-generated)
        noc_result = test_user1.get_matter_noc(group_id)
        assert noc_result is not None, "Failed to get Matter NOC"
        assert "noc" in noc_result, "Response should contain 'noc'"
        assert "matter_node_id" in noc_result, "Response should contain 'matter_node_id'"

        user_noc = noc_result["noc"]
        matter_node_id = noc_result["matter_node_id"]

        # Validate User NOC is signed by group's Root CA
        assert verify_certificate_signed_by(user_noc, root_ca), "User NOC should be signed by group's Root CA"

        # Validate User NOC contains correct Matter OIDs
        noc_oids = extract_matter_oids(user_noc)
        assert noc_oids["fabric_id"] is not None, "User NOC should contain Fabric ID OID"
        assert noc_oids["node_id"] is not None, "User NOC should contain Node ID OID"
        assert noc_oids["cat_id"] is not None, "User NOC should contain CAT ID OID (admin or operate)"

        # Verify Node ID matches the canonical response field.
        # Node ID in cert may be in different format, compare the values
        assert noc_oids["node_id"] == matter_node_id

        # Simulate two phones by generating two independent operational keys.
        def phone_csr():
            key = ec.generate_private_key(ec.SECP256R1())
            csr = x509.CertificateSigningRequestBuilder().subject_name(
                x509.Name([])
            ).sign(key, hashes.SHA256())
            return csr.public_bytes(serialization.Encoding.PEM).decode("utf-8")

        first_phone_csr = phone_csr()
        second_phone_csr = phone_csr()
        first_phone = test_user1.get_matter_noc(group_id, first_phone_csr)
        first_phone_retry = test_user1.get_matter_noc(group_id, first_phone_csr)
        second_phone = test_user1.get_matter_noc(group_id, second_phone_csr)
        assert first_phone["matter_node_id"] == first_phone_retry["matter_node_id"]
        assert first_phone["matter_node_id"] != second_phone["matter_node_id"]

    finally:
        if group_id:
            user1_group_api.delete_group(group_id)


def test_matter_cat_id_version_increment_on_unshare(test_user1, test_user2):
    """Test that CAT ID version is incremented when a user is unshared from a Matter group."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = None

    try:
        # Create Matter group
        response_data = user1_group_api.create_matter_group("Test Matter CAT ID Version Group")
        group_id = response_data["group_id"]
        matter = response_data["matter"]
        initial_operate_cat_id = matter["group_cat_id_operate"]
        initial_admin_cat_id = matter["group_cat_id_admin"]
        root_ca = matter["root_ca"]

        print(f"Initial operate CAT ID: {initial_operate_cat_id}")
        print(f"Initial admin CAT ID: {initial_admin_cat_id}")

        # Share group with user2 (secondary access)
        user1_group_api.share_group(group_id, test_user2.username, "secondary")
        accept_sharing_request_for(test_user2, group_id, "")

        # Verify user2 can access the group
        user2_groups = user2_group_api.list_groups()
        assert any(g['group_id'] == group_id for g in user2_groups['groups']), "Shared group not found for user2"

        # Generate NOC for user2 and verify CAT ID
        noc_result = test_user2.get_matter_noc(group_id)
        assert noc_result is not None, "Failed to get Matter NOC for user2"
        user2_noc = noc_result["noc"]

        # Extract CAT ID from user2's NOC
        noc_oids = extract_matter_oids(user2_noc)
        assert noc_oids["cat_id"] == initial_operate_cat_id, \
            f"User2 NOC should have initial operate CAT ID. Expected {initial_operate_cat_id}, got {noc_oids['cat_id']}"

        # Unshare group with user2
        user1_group_api.unshare_group(group_id, test_user2.user_id)

        # Re-fetch group details and verify operate CAT ID version incremented
        user1_groups = user1_group_api.list_groups()
        updated_group = next((g for g in user1_groups['groups'] if g['group_id'] == group_id), None)
        assert updated_group is not None, "Group not found in response"
        updated_matter = updated_group["matter"]
        updated_operate_cat_id = updated_matter["group_cat_id_operate"]
        updated_admin_cat_id = updated_matter["group_cat_id_admin"]

        print(f"Updated operate CAT ID: {updated_operate_cat_id}")
        print(f"Updated admin CAT ID: {updated_admin_cat_id}")

        # Verify operate CAT ID version was incremented (identifier stays same, version increases by 1)
        assert updated_operate_cat_id[:4] == initial_operate_cat_id[:4], \
            f"Operate CAT ID identifier should not change. Expected {initial_operate_cat_id[:4]}, got {updated_operate_cat_id[:4]}"
        initial_version = int(initial_operate_cat_id[4:], 16)
        updated_version = int(updated_operate_cat_id[4:], 16)
        assert updated_version == initial_version + 1, \
            f"Operate CAT ID version should be incremented by 1. Expected {initial_version + 1}, got {updated_version}"

        # Verify admin CAT ID was NOT changed (user2 had secondary access)
        assert updated_admin_cat_id == initial_admin_cat_id, \
            f"Admin CAT ID should not change. Expected {initial_admin_cat_id}, got {updated_admin_cat_id}"

        # Generate new NOC for user1 (still in group) and verify it has updated CAT ID
        noc_result_user1 = test_user1.get_matter_noc(group_id)
        assert noc_result_user1 is not None, "Failed to get Matter NOC for user1"
        user1_noc = noc_result_user1["noc"]

        # User1 has primary access, so should get admin CAT ID (not operate)
        noc_oids_user1 = extract_matter_oids(user1_noc)
        assert noc_oids_user1["cat_id"] == updated_admin_cat_id, \
            f"User1 NOC should have admin CAT ID. Expected {updated_admin_cat_id}, got {noc_oids_user1['cat_id']}"

        # Verify NOC is signed by group's Root CA
        assert verify_certificate_signed_by(user1_noc, root_ca), "User1 NOC should be signed by group's Root CA"

    finally:
        if group_id:
            user1_group_api.delete_group(group_id)


def test_matter_admin_cat_id_increment_on_primary_unshare(test_user1, test_user2):
    """Test that admin CAT ID version is incremented when a user with primary access is unshared."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = None

    try:
        # Create Matter group
        response_data = user1_group_api.create_matter_group("Test Matter Admin CAT ID Group")
        group_id = response_data["group_id"]
        matter = response_data["matter"]
        initial_operate_cat_id = matter["group_cat_id_operate"]
        initial_admin_cat_id = matter["group_cat_id_admin"]

        print(f"Initial operate CAT ID: {initial_operate_cat_id}")
        print(f"Initial admin CAT ID: {initial_admin_cat_id}")

        # Share group with user2 (PRIMARY access)
        user1_group_api.share_group(group_id, test_user2.username, "primary")
        accept_sharing_request_for(test_user2, group_id, "")

        # Verify user2 can access the group
        user2_groups = user2_group_api.list_groups()
        assert any(g['group_id'] == group_id for g in user2_groups['groups']), "Shared group not found for user2"

        # Unshare group with user2
        user1_group_api.unshare_group(group_id, test_user2.user_id)

        # Re-fetch group details
        user1_groups = user1_group_api.list_groups()
        updated_group = next((g for g in user1_groups['groups'] if g['group_id'] == group_id), None)
        assert updated_group is not None, "Group not found in response"
        updated_matter = updated_group["matter"]
        updated_operate_cat_id = updated_matter["group_cat_id_operate"]
        updated_admin_cat_id = updated_matter["group_cat_id_admin"]

        print(f"Updated operate CAT ID: {updated_operate_cat_id}")
        print(f"Updated admin CAT ID: {updated_admin_cat_id}")

        # Verify admin CAT ID version was incremented (user2 had primary access)
        assert updated_admin_cat_id[:4] == initial_admin_cat_id[:4], \
            f"Admin CAT ID identifier should not change. Expected {initial_admin_cat_id[:4]}, got {updated_admin_cat_id[:4]}"
        initial_admin_version = int(initial_admin_cat_id[4:], 16)
        updated_admin_version = int(updated_admin_cat_id[4:], 16)
        assert updated_admin_version == initial_admin_version + 1, \
            f"Admin CAT ID version should be incremented by 1. Expected {initial_admin_version + 1}, got {updated_admin_version}"

        # Verify operate CAT ID was NOT changed
        assert updated_operate_cat_id == initial_operate_cat_id, \
            f"Operate CAT ID should not change. Expected {initial_operate_cat_id}, got {updated_operate_cat_id}"

    finally:
        if group_id:
            user1_group_api.delete_group(group_id)


def test_matter_dev_assoc(matter_group, session_valid_device_ec):
    """Test full device association flow for Matter groups using nocsr_elements flow.

    Scenario: Device with registered private key that signs attestation correctly.
    The device's certificate is registered with IoT Core, and the attestation signature
    is verified against that certificate.
    """
    user = matter_group["user"]
    device = session_valid_device_ec
    group_id = matter_group["group_id"]
    root_ca = matter_group["root_ca"]
    group_api = matter_group["group_api"]

    assert connect_device_with_retry(device), "Failed to connect the device"

    # Use the full flow helper with device key signing and vendor_reserved1
    result = do_matter_dev_assoc(
        user,
        device,
        group_id,
        use_device_key=True,  # Sign with device's registered key
        include_vendor_reserved1=True  # Include node_id in vendor_reserved1
    )

    assert isinstance(result, dict), f"Matter association failed: {result}"
    assert "noc" in result, "Result should contain 'noc'"

    device_noc = result["noc"]
    matter_node_id = result.get("matter_node_id")

    # Validate Device NOC is signed by group's Root CA
    assert verify_certificate_signed_by(device_noc, root_ca), "Device NOC should be signed by group's Root CA"

    # Validate Device NOC contains correct Matter OIDs
    device_noc_oids = extract_matter_oids(device_noc)
    assert device_noc_oids["fabric_id"] is not None, "Device NOC should contain Fabric ID OID"
    assert device_noc_oids["node_id"] is not None, "Device NOC should contain Node ID OID"
    # Devices should NOT have CAT ID
    assert device_noc_oids["cat_id"] is None, "Device NOC should NOT contain CAT ID OID"

    print(f"Device NOC Node ID: {device_noc_oids['node_id']}, matter_node_id: {matter_node_id}")

    # Verify node appears in group listing
    assert device.wait_for_group_info(), "Device failed to receive group info"
    assert device.group_id == group_id, f"Device group_id mismatch: expected {group_id}, got {device.group_id}"

    groups_data = group_api.list_groups()
    group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert group is not None, f"Group {group_id} not found"
    assert "node_ids" in group, "Group should have node_ids list"

    # A registered device that joined the fabric is tagged both "rmng" and "matter",
    # and its matter_node_id is exposed (derived from node_id).
    node_detail = group.get("node_details", {}).get(device.node_thing_name, {})
    assert "rmng" in node_detail.get("capabilities", []), f"Expected 'rmng' in {node_detail.get('capabilities')}"
    assert "matter" in node_detail.get("capabilities", []), f"Expected 'matter' in {node_detail.get('capabilities')}"
    assert node_detail.get("capability_details", {}).get("matter", {}).get("matter_node_id"), \
        "Matter node should expose matter_node_id under capability_details"

    device.disconnect()


def test_matter_node_remove_from_grp(matter_group, session_valid_device_ec):
    """Test removing a Matter node from a group after Matter device association.

    Scenario: A device is associated to a Matter group via the nocsr_elements flow
    (fabric node with NOC). Then the node is removed from the group via disassociation.
    Verifies:
      - Node is successfully removed from the group
      - Group still exists after node removal
    """
    user = matter_group["user"]
    device = session_valid_device_ec
    group_id = matter_group["group_id"]
    root_ca = matter_group["root_ca"]
    group_api = matter_group["group_api"]

    assert connect_device_with_retry(device), "Failed to connect the device"

    # Associate Matter node via nocsr_elements flow
    result = do_matter_dev_assoc(
        user,
        device,
        group_id,
        use_device_key=True,
        include_vendor_reserved1=True,
    )
    assert isinstance(result, dict), f"Matter association failed: {result}"
    assert "noc" in result, "Result should contain 'noc'"

    node_id = device.node_thing_name

    # Verify node is in the group
    assert device.wait_for_group_info(), "Device failed to receive group info"
    groups_data = group_api.list_groups()
    grp = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert grp is not None, f"Group {group_id} not found"
    assert node_id in grp.get("node_ids", []), f"Node {node_id} should be in the group"

    device.disconnect()

    # Remove node from group
    group_api.remove_node_from_group(group_id, node_id)

    # Verify node is removed from the group
    groups_data = group_api.list_groups()
    grp = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert grp is not None, "Group should still exist after node removal"
    assert node_id not in grp.get("node_ids", []), \
        f"Node {node_id} should NOT be in the group after removal"


def test_matter_delete_group_with_fabric_node(test_user1, session_valid_device_ec):
    """Test deleting a Matter group that has a fabric node associated.

    Scenario: A device is associated to a Matter group via the nocsr_elements flow.
    Then the entire group is deleted. Verifies:
      - Group is successfully deleted
      - Node is no longer in any group
    """
    user = test_user1
    device = session_valid_device_ec
    group_api = Group(user)

    assert connect_device_with_retry(device), "Failed to connect the device"

    # Create Matter group
    response_data = group_api.create_matter_group("Delete Matter Group Test")
    group_id = response_data["group_id"]

    # Associate Matter node via nocsr_elements flow
    result = do_matter_dev_assoc(
        user,
        device,
        group_id,
        use_device_key=True,
        include_vendor_reserved1=True,
    )
    assert isinstance(result, dict), f"Matter association failed: {result}"
    assert "noc" in result, "Result should contain 'noc'"

    node_id = device.node_thing_name

    # Verify node is in the group
    assert device.wait_for_group_info(), "Device failed to receive group info"
    groups_data = group_api.list_groups()
    grp = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert grp is not None, f"Group {group_id} not found"
    assert node_id in grp.get("node_ids", []), f"Node {node_id} should be in the group"

    device.disconnect()

    # Delete the group
    group_api.delete_group(group_id)

    # Verify group no longer exists
    groups_data = group_api.list_groups()
    grp = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert grp is None, f"Group {group_id} should not exist after deletion"


def test_matter_dev_assoc_sig_mismatch(matter_group, session_valid_device_ec):
    """Test Matter attestation fails when signature doesn't match registered certificate.

    Scenario: Device with registered private key, but the attestation signature is signed
    with a DIFFERENT key (not the device's registered key). The verification should fail
    because the signature doesn't match any certificate registered for the node_id.
    """
    user = matter_group["user"]
    device = session_valid_device_ec
    group_id = matter_group["group_id"]

    # Use the full flow helper with WRONG key but WITH vendor_reserved1
    # This means vendor_reserved1 identifies the device, but signature is from different key
    result = do_matter_dev_assoc(
        user,
        device,
        group_id,
        use_device_key=False,  # Sign with random key (NOT device's key)
        include_vendor_reserved1=True  # Include node_id - enables signature verification
    )

    # Should fail because signature doesn't match the registered certificate
    assert isinstance(result, str), f"Expected error for signature mismatch, got success: {result}"
    assert "ERROR_VERIFY_FAILED" in result, f"Expected verify failure, got: {result}"

    print(f"Correctly rejected attestation with mismatched signature: {result}")


def test_matter_noc_without_csr_fails(test_user1):
    """Test that requesting User NOC without CSR returns 400 error."""
    user1_group_api = Group(test_user1)
    group_id = None

    try:
        # Create Matter group
        response_data = user1_group_api.create_matter_group("Test Matter NOC No CSR")
        group_id = response_data["group_id"]

        # Request User NOC without CSR
        noc_response = test_user1.get_matter_noc_raw(group_id)
        assert noc_response.status_code == 400, f"Expected 400, got {noc_response.status_code}: {noc_response.text}"

    finally:
        if group_id:
            user1_group_api.delete_group(group_id)


def test_matter_dev_assoc_pure_rm_node(matter_group, session_valid_device_ec):
    """Test that challenge_response on Matter group succeeds (adds node to group without NOC).

    Scenario: Device uses traditional challenge_response authentication (not nocsr_elements).
    The device signs the challenge with its registered private key. On Matter groups,
    this adds the node to the group but does NOT generate a Matter NOC.
    This is useful for legacy devices that want to join Matter groups.
    """
    user = matter_group["user"]
    device = session_valid_device_ec
    group_id = matter_group["group_id"]
    group_api = matter_group["group_api"]

    assert connect_device_with_retry(device), "Failed to connect the device"

    # Associate using challenge_response (should succeed - adds node to group without NOC)
    assert user.do_user_node_assoc(device, group_id) == None, "Failed to associate device with group"# Uses challenge_response flow

    # Verify node appears in group listing
    assert device.wait_for_group_info(), "Device failed to receive group info"
    assert device.group_id == group_id, f"Device group_id mismatch: expected {group_id}, got {device.group_id}"

    groups_data = group_api.list_groups()
    group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert group is not None, f"Group {group_id} not found"
    assert "node_ids" in group, "Group should have node_ids list"
    assert device.node_thing_name in group["node_ids"], \
        f"Device {device.node_thing_name} should appear in group node_ids"

    # A challenge_response node is a plain RainMaker node: tagged "rmng", not "matter".
    node_detail = group.get("node_details", {}).get(device.node_thing_name, {})
    assert node_detail.get("capabilities") == ["rmng"], \
        f"Expected ['rmng'], got {node_detail.get('capabilities')}"

    print(f"Successfully added node {device.node_thing_name} to Matter group using challenge_response")

    device.disconnect()

def test_matter_noc_non_matter_group_fails(test_user1):
    """Test that NOC endpoint rejects non-Matter groups."""
    user1_group_api = Group(test_user1)
    group_id = None

    try:
        # Create regular group (no Matter capability)
        group_id = user1_group_api.create_group("Test Non-Matter Group")

        # Generate CSR and request NOC on non-Matter group (should fail)
        csr_pem = test_user1.generate_csr()
        noc_response = test_user1.get_matter_noc_raw(group_id, json.dumps({"csr": csr_pem}))
        assert noc_response.status_code == 400, f"Expected 400 for non-Matter group, got {noc_response.status_code}: {noc_response.text}"

    finally:
        if group_id:
            user1_group_api.delete_group(group_id)


def _verify_pure_matter_node_result(result, root_ca, vendor_reserved1_ref):
    """Helper to verify pure Matter node result.

    Args:
        result: Result dict from do_matter_dev_assoc
        root_ca: Root CA certificate for NOC verification
        vendor_reserved1_ref: Reference value to ensure matter_node_id is different from
    """
    assert isinstance(result, dict), f"Pure Matter node should succeed, got: {result}"
    assert "noc" in result, "Result should contain 'noc'"
    assert "matter_node_id" in result, "Result should contain 'matter_node_id'"

    # Validate NOC is signed by group's Root CA
    assert verify_certificate_signed_by(result["noc"], root_ca), \
        "Device NOC should be signed by group's Root CA"

    # Ensure the generated matter_node_id is random and doesn't match vendor_reserved1
    assert result["matter_node_id"] != vendor_reserved1_ref, \
        f"matter_node_id should be random, not match vendor_reserved1: {vendor_reserved1_ref}"

    print(f"Pure Matter node attestation successful, matter_node_id: {result['matter_node_id']}")


def test_matter_dev_assoc_pure_matter_node(matter_group, unregistered_device_ec):
    """Test Matter attestation for pure Matter node (no vendor_reserved1).

    Scenario: Pure Matter node with no vendor_reserved1 in NOCSRElements.
    The signature is not verified because there's no way to identify the device.
    A random node_id is generated for the device.
    """
    user = matter_group["user"]
    device = unregistered_device_ec
    group_id = matter_group["group_id"]
    root_ca = matter_group["root_ca"]
    group_api = matter_group["group_api"]

    # Use the full flow helper WITHOUT device key and WITHOUT vendor_reserved1
    result = do_matter_dev_assoc(
        user,
        device,
        group_id,
        use_device_key=False,  # Use random key (signature not verified for pure Matter)
        include_vendor_reserved1=False  # No vendor_reserved1 = pure Matter node
    )

    _verify_pure_matter_node_result(result, root_ca, device.node_thing_name)

    # A pure Matter node is stored under its generated matter_node_id and is tagged
    # "matter" only (no "rmng", since it is not a registered RainMaker device).
    matter_node_id = result["matter_node_id"]
    groups_data = group_api.list_groups()
    group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert group is not None, f"Group {group_id} not found"
    node_detail = group.get("node_details", {}).get(matter_node_id, {})
    assert node_detail.get("capabilities") == ["matter"], \
        f"Pure Matter node should be ['matter'], got {node_detail.get('capabilities')}"
    assert node_detail.get("capability_details", {}).get("matter", {}).get("matter_node_id") == matter_node_id, \
        "Pure Matter node matter_node_id should match the generated id"


def test_matter_dev_assoc_pure_matter_node_thing_name_not_found(matter_group, unregistered_device_ec):
    """Test Matter attestation with vendor_reserved1 that doesn't match any registered device.

    Scenario: NOCSRElements contains vendor_reserved1 but the thing name doesn't match
    any device registered in the cloud. This is also treated as a pure Matter node.
    A random node_id is generated for the device.
    """
    user = matter_group["user"]
    device = unregistered_device_ec
    group_id = matter_group["group_id"]
    root_ca = matter_group["root_ca"]

    # Use a thing name that doesn't exist in the cloud
    non_existent_thing_name = f"non_existent_thing_{uuid.uuid4().hex[:8]}"

    # Use the full flow helper with a custom vendor_reserved1 that doesn't match any registered device
    result = do_matter_dev_assoc(
        user,
        device,
        group_id,
        use_device_key=False,  # Use random key (signature not verified for pure Matter)
        include_vendor_reserved1=True,
        custom_vendor_reserved1=non_existent_thing_name  # Thing name not found in cloud
    )

    _verify_pure_matter_node_result(result, root_ca, non_existent_thing_name)


def _sample_matter_config():
    """A well-formed pure-Matter NodeConfig an app would PUT."""
    return {
        "data_model": "matter",
        "info": {
            "name": "Living Room Light",
            "type": "matter",
            "fw_version": "1.0",
            "model": "0x010D",
        },
        "endpoints": {
            "0x0": {
                "dt": "0x0016",
                "c": {"s": {"0x1d": {}, "0x28": {}, "0x1f": {}, "0x3e": {}}},
            },
            "0x1": {
                "dt": "0x010D",
                "c": {
                    "s": {
                        "0x3": {},
                        "0x4": {},
                        "0x6": {"a": ["0x0"]},
                        "0x8": {"a": ["0x0"]},
                        "0x300": {"a": ["0x7", "0x8", "0x0f"]},
                        "0x1d": {},
                    }
                },
            },
        },
    }


def test_matter_config_write_and_read_pure_matter_node(matter_group, unregistered_device_ec):
    """An app can write and read back the config of a pure Matter node.

    A pure Matter node has no RainMaker firmware to publish its own config, so the app
    supplies the Matter config over PUT /config; GET /config then round-trips it.
    """
    user = matter_group["user"]
    device = unregistered_device_ec
    group_id = matter_group["group_id"]

    result = do_matter_dev_assoc(
        user,
        device,
        group_id,
        use_device_key=False,
        include_vendor_reserved1=False,
    )
    matter_node_id = result["matter_node_id"]

    config = _sample_matter_config()
    put_resp = user.put_node_config(group_id, matter_node_id, config)
    assert put_resp.status_code == 200, \
        f"Expected 200 writing pure Matter config, got {put_resp.status_code}: {put_resp.text}"

    stored = user.get_node_config(group_id, None, matter_node_id)
    assert stored is not None, "Config should be readable after write"
    assert stored.get("data_model") == "matter"
    assert stored.get("endpoints") == config["endpoints"]
    assert stored.get("info") == config["info"]


def test_matter_config_write_rejected_for_rm_node(matter_group, session_valid_device_ec):
    """PUT /config is rejected for a RainMaker node so an app cannot overwrite firmware config."""
    user = matter_group["user"]
    device = session_valid_device_ec
    group_id = matter_group["group_id"]

    assert connect_device_with_retry(device), "Failed to connect the device"
    assert user.do_user_node_assoc(device, group_id) is None, "Failed to associate device with group"

    resp = user.put_node_config(group_id, device.node_thing_name, _sample_matter_config())
    assert resp.status_code == 403, \
        f"Expected 403 writing config for RainMaker node, got {resp.status_code}: {resp.text}"

    device.disconnect()

def test_matter_attestation_missing_fields(test_user1, unregistered_device_ec):
    """Test that verify fails when attestation fields are incomplete (nocsr_elements without attestation fields)."""
    user1_group_api = Group(test_user1)
    device = unregistered_device_ec
    group_id = None

    try:
        # Create Matter group
        response_data = user1_group_api.create_matter_group("Test Attestation Missing Fields")
        group_id = response_data["group_id"]

        # Initiate
        request_id, challenge = do_initiate(test_user1, group_id)
        assert request_id is not None, f"Initiate failed: {challenge}"

        # Build NOCSRElements with correct CSRNonce (from challenge)
        csr_nonce = bytes.fromhex(challenge)
        _, csr_der = device.generate_csr()
        nocsr_elements = build_nocsr_elements_tlv(csr_der, csr_nonce, None)

        # Verify with nocsr_elements but MISSING attestation_challenge and attestation_signature
        # Note: challenge_response and nocsr_elements are now mutually exclusive
        verify_payload = {
            "nocsr_elements": nocsr_elements.hex(),
            # Missing: "attestation_challenge" and "attestation_signature"
        }

        verify_response = test_user1.make_api_request('POST', f'/v1/groups/{group_id}/node-assoc-requests/{request_id}/verify', data=json.dumps(verify_payload))
        assert verify_response.status_code == 400, \
            f"Expected 400 for missing attestation fields, got {verify_response.status_code}: {verify_response.text}"

    finally:
        if group_id:
            user1_group_api.delete_group(group_id)


def test_matter_attestation_csr_nonce_mismatch(test_user1, unregistered_device_ec):
    """Test that verify fails when CSRNonce doesn't match the challenge from initiate.

    This validates that the backend correctly verifies the CSRNonce in NOCSRElements
    matches the challenge returned during the initiate step.
    """
    user1_group_api = Group(test_user1)
    device = unregistered_device_ec
    group_id = None

    try:
        # Create Matter group
        response_data = user1_group_api.create_matter_group("Test CSRNonce Mismatch")
        group_id = response_data["group_id"]

        # Initiate - get the challenge
        request_id, challenge = do_initiate(test_user1, group_id)
        assert request_id is not None, f"Initiate failed: {challenge}"

        # Build NOCSRElements with WRONG CSRNonce (random instead of challenge)
        wrong_csr_nonce = os.urandom(32)  # This should NOT match the challenge

        _, csr_der = device.generate_csr()
        nocsr_elements = build_nocsr_elements_tlv(csr_der, wrong_csr_nonce, None)

        # Generate attestation challenge and sign
        attestation_challenge = os.urandom(16)
        random_key = ec.generate_private_key(ec.SECP256R1())
        attestation_signature = sign_attestation_data(nocsr_elements, attestation_challenge, random_key)

        # Verify should fail due to CSRNonce mismatch
        verify_result, error = do_verify_with_nocsr_elements(
            test_user1, group_id, request_id, nocsr_elements, attestation_challenge, attestation_signature
        )

        # Should fail with CSRNonce mismatch error
        assert error is not None, f"Expected CSRNonce mismatch error, got success: {verify_result}"
        assert "ERROR_VERIFY_FAILED" in error, f"Expected verify failure, got: {error}"

        print(f"Correctly rejected request with CSRNonce mismatch: {error}")

    finally:
        if group_id:
            user1_group_api.delete_group(group_id)


def test_convert_group_to_fabric(test_user1, test_user2, test_user3):
    """A plain group can be converted into a Matter fabric by its owner, after which all
    members can obtain NOCs — both users shared in before the conversion (provisioned via
    backfill) and users shared in afterwards."""
    user1_group_api = Group(test_user1)
    group_id = None

    try:
        group_id = user1_group_api.create_group("Group To Convert")

        # Share with user2 before conversion.
        user1_group_api.share_group(group_id, test_user2.username, "secondary")
        accept_sharing_request_for(test_user2, group_id, "")

        # Convert the plain group into a Matter fabric.
        resp = user1_group_api.add_group_capabilities(group_id, ["matter"])
        assert resp.status_code == 200, f"Expected 200, got {resp.status_code}: {resp.text}"
        matter = resp.json().get("matter", {})
        assert matter.get("fabric_id"), "Converted group should expose a fabric_id"
        root_ca = matter.get("root_ca")
        assert root_ca, "Converted group should expose a root_ca"

        # The group now reports the matter capability when listed.
        groups_data = user1_group_api.list_groups()
        grp = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
        assert grp is not None and grp.get("matter"), "Listed group should carry matter data after conversion"

        # The owner can obtain a NOC on the freshly-converted fabric.
        owner_noc = test_user1.get_matter_noc(group_id)
        assert owner_noc and owner_noc.get("noc"), "Owner should get a NOC after conversion"
        assert verify_certificate_signed_by(owner_noc["noc"], root_ca), \
            "Owner NOC should be signed by the fabric Root CA"

        # The user shared in before conversion can obtain a NOC without per-user
        # Matter backfill. Secondary access => operate CAT ID.
        pre_noc = test_user2.get_matter_noc(group_id)
        assert pre_noc and pre_noc.get("noc"), "Pre-conversion member should get a NOC after conversion"
        assert pre_noc.get("matter_node_id"), "Pre-conversion member NOC should include a matter_node_id"
        assert verify_certificate_signed_by(pre_noc["noc"], root_ca), \
            "Pre-conversion member NOC should be signed by the fabric Root CA"
        pre_oids = extract_matter_oids(pre_noc["noc"])
        assert pre_oids["cat_id"] == matter["group_cat_id_operate"], \
            f"Pre-conversion (secondary) member NOC should carry the operate CAT ID, got {pre_oids['cat_id']}"

        # A user shared in AFTER conversion can also obtain a NOC.
        user1_group_api.share_group(group_id, test_user3.username, "secondary")
        accept_sharing_request_for(test_user3, group_id, "")
        post_noc = test_user3.get_matter_noc(group_id)
        assert post_noc and post_noc.get("noc"), "Post-conversion member should get a NOC"
        assert verify_certificate_signed_by(post_noc["noc"], root_ca), \
            "Post-conversion member NOC should be signed by the fabric Root CA"

    finally:
        if group_id:
            user1_group_api.delete_group(group_id)


def test_convert_group_with_shared_subgroup(test_user1, test_user2, valid_device):
    """A group with a shared subgroup can still be converted to a Matter fabric."""
    user1_group_api = Group(test_user1)
    group_id = None

    try:
        group_id = user1_group_api.create_group("Group With Shared Subgroup")

        # Create a subgroup, add a node, and share it with user2 (subentity access).
        subgroup_id = user1_group_api.create_subgroup(group_id, "Shared Subgroup")
        result = test_user1.do_user_node_assoc(valid_device, group_id)
        assert result is None, f"Association failed: {result}"
        user1_group_api.add_node_to_subgroup(group_id, subgroup_id, valid_device.node_thing_name)
        user1_group_api.share_subgroup(group_id, subgroup_id, test_user2.username)
        accept_sharing_request_for(test_user2, group_id, subgroup_id)

        # Conversion succeeds despite the shared subgroup.
        resp = user1_group_api.add_group_capabilities(group_id, ["matter"])
        assert resp.status_code == 200, f"Expected 200, got {resp.status_code}: {resp.text}"
        assert resp.json().get("matter", {}).get("fabric_id"), "Converted group should expose a fabric_id"

        # The subgroup-shared user only has subentity access, not fabric-wide group
        # access, so they cannot obtain a Matter NOC on the converted fabric.
        noc_response = test_user2.get_matter_noc_raw(group_id, json.dumps({"csr": test_user2.generate_csr()}))
        assert noc_response.status_code != 200, \
            f"Subgroup-shared user should not get a NOC, got {noc_response.status_code}: {noc_response.text}"

    finally:
        if group_id:
            user1_group_api.delete_group(group_id)


def test_convert_group_to_fabric_secondary_user_denied(test_user1, test_user2):
    """A non-owner (secondary) user cannot convert a group to a fabric."""
    user1_group_api = Group(test_user1)
    group_id = None

    try:
        group_id = user1_group_api.create_group("Owner Only Convert")

        user1_group_api.share_group(group_id, test_user2.username, "secondary")
        accept_sharing_request_for(test_user2, group_id, "")

        user2_group_api = Group(test_user2)
        resp = user2_group_api.add_group_capabilities(group_id, ["matter"])
        assert resp.status_code != 200, f"Expected failure, got {resp.status_code}: {resp.text}"

    finally:
        if group_id:
            user1_group_api.delete_group(group_id)


def test_matter_noc_on_foreign_group_denied(test_user1, test_user2):
    """A must not mint a Matter NOC (device operational cert) against B's group.

    A NOC binds a device into a Matter fabric; issuing one against a foreign
    fabric is cross-tenant certificate issuance.
    """
    user_a, user_b = test_user1, test_user2
    group_api_b = Group(user_b)
    group_id_b = None
    try:
        resp_b = group_api_b.create_matter_group("PEsc Matter B")
        group_id_b = resp_b["group_id"]

        noc = user_a.get_matter_noc(group_id_b)
        assert noc is None, (
            "A obtained a Matter NOC issued against a foreign tenant's fabric "
            "(cross-tenant device certificate issuance)"
        )
    finally:
        if group_id_b:
            group_api_b.delete_group(group_id_b, warn_error=True)
