# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Matter attestation helper functions for testing.

This module provides helper functions for testing Matter device association
using the nocsr_elements flow with attestation verification.
"""

import json
import os
import struct

from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives.asymmetric.utils import decode_dss_signature


def build_nocsr_elements_tlv(csr_der: bytes, csr_nonce: bytes, vendor_reserved1: bytes = None) -> bytes:
    """Build a NOCSRElements TLV structure for Matter attestation.

    Args:
        csr_der: DER-encoded Certificate Signing Request
        csr_nonce: 32-byte CSR nonce
        vendor_reserved1: Optional vendor reserved field (contains nodeID for RainMaker)

    Returns:
        bytes: TLV-encoded NOCSRElements structure
    """
    result = bytearray()

    # Structure start (0x15)
    result.append(0x15)

    # CSR (tag 0x01) with 2-byte length octet string
    # Control byte: context-specific (0x20) + 2-byte length type (0x11) = 0x31
    result.append(0x31)
    result.append(0x01)  # Tag value
    result.extend(struct.pack('<H', len(csr_der)))  # 2-byte little-endian length
    result.extend(csr_der)

    # CSRNonce (tag 0x02) with 1-byte length octet string
    # Control byte: context-specific (0x20) + 1-byte length type (0x10) = 0x30
    result.append(0x30)
    result.append(0x02)  # Tag value
    result.append(len(csr_nonce))  # 1-byte length
    result.extend(csr_nonce)

    # VendorReserved1 (tag 0x03) with 1-byte length octet string (optional)
    if vendor_reserved1 is not None:
        result.append(0x30)
        result.append(0x03)  # Tag value
        result.append(len(vendor_reserved1))  # 1-byte length
        result.extend(vendor_reserved1)

    # Structure end (0x18)
    result.append(0x18)

    return bytes(result)


def sign_attestation_data(nocsr_elements: bytes, attestation_challenge: bytes, private_key) -> bytes:
    """Sign the TBS data (NOCSRElements || AttestationChallenge) with the given private key.

    Args:
        nocsr_elements: TLV-encoded NOCSRElements
        attestation_challenge: 16-byte attestation challenge
        private_key: EC private key for signing

    Returns:
        bytes: 64-byte raw r||s ECDSA signature
    """
    import hashlib

    # Build TBS data
    tbs = nocsr_elements + attestation_challenge

    # Compute SHA256 hash
    hash_digest = hashlib.sha256(tbs).digest()

    # Sign with ECDSA using prehashed data
    from cryptography.hazmat.primitives.asymmetric.utils import Prehashed

    der_sig = private_key.sign(hash_digest, ec.ECDSA(Prehashed(hashes.SHA256())))

    # Convert DER signature to raw r||s format (64 bytes)
    r, s = decode_dss_signature(der_sig)

    # Pad r and s to 32 bytes each
    r_bytes = r.to_bytes(32, byteorder='big')
    s_bytes = s.to_bytes(32, byteorder='big')

    return r_bytes + s_bytes


def do_initiate(user, group_id):
    """Initiate association and return request_id and challenge.

    Returns:
        tuple (request_id, challenge) on success, or (None, error_string) on failure
    """
    initiate_response = user.make_api_request('POST', f'/v1/groups/{group_id}/node-assoc-requests', data='{}')
    if initiate_response.status_code not in (200, 201):
        return None, f"ERROR_INITIATE_FAILED_{initiate_response.status_code}"

    initiate_result = json.loads(initiate_response.text)
    request_id = initiate_result.get('request_id')
    challenge = initiate_result.get('challenge')
    if not request_id or not challenge:
        return None, "ERROR_INVALID_INITIATE_RESPONSE"

    return request_id, challenge


def do_verify_with_nocsr_elements(user, group_id, request_id, nocsr_elements: bytes,
                                   attestation_challenge: bytes, attestation_signature: bytes):
    """Perform Matter verify with pre-built NOCSRElements.

    Args:
        user: User object with make_api_request method
        group_id: ID of the Matter group
        request_id: Request ID from initiate
        nocsr_elements: TLV-encoded NOCSRElements
        attestation_challenge: 16-byte attestation challenge
        attestation_signature: 64-byte raw r||s signature

    Returns:
        tuple (verify_result_dict, None) on success, or (None, error_string) on failure
    """
    verify_payload = {
        "nocsr_elements": nocsr_elements.hex(),
        "attestation_challenge": attestation_challenge.hex(),
        "attestation_signature": attestation_signature.hex(),
    }

    verify_response = user.make_api_request('POST', f'/v1/groups/{group_id}/node-assoc-requests/{request_id}/verify', data=json.dumps(verify_payload))
    if verify_response.status_code not in (200, 201):
        return None, f"ERROR_VERIFY_FAILED_{verify_response.status_code}"

    verify_result = json.loads(verify_response.text)
    if verify_result.get('message') != 'success':
        return None, f"ERROR_VERIFY_FAILED_{verify_result.get('message')}"

    return verify_result, None


def do_confirm(user, group_id, request_id, capabilities=None):
    """Confirm the association.

    Args:
        user: User object with make_api_request method
        group_id: ID of the group
        request_id: Request ID from initiate
        capabilities: Optional list of capability names to enable

    Returns:
        True on success, error string on failure
    """
    payload = {}
    if capabilities:
        payload['capabilities'] = capabilities

    confirm_response = user.make_api_request(
        'POST',
        f'/v1/groups/{group_id}/node-assoc-requests/{request_id}/confirm',
        data=json.dumps(payload) if payload else '{}'
    )
    if confirm_response.status_code not in (200, 201):
        return f"ERROR_CONFIRM_FAILED_{confirm_response.status_code}"
    return True


def do_matter_dev_assoc(user, device, group_id, use_device_key=False, include_vendor_reserved1=True, custom_vendor_reserved1=None, capabilities=None):
    """Full Matter device association flow using nocsr_elements.

    This function handles the complete flow:
    1. Initiate - get challenge
    2. Build NOCSRElements with challenge as CSRNonce
    3. Sign attestation (with device key if use_device_key=True, otherwise random key)
    4. Verify
    5. Confirm

    Args:
        user: User object with make_api_request method
        device: Device object (used for vendor_reserved1 and optionally for signing)
        group_id: ID of the Matter group
        use_device_key: If True, sign with device's registered private key; if False, use random key
        include_vendor_reserved1: If True, include vendor_reserved1 in NOCSRElements
        custom_vendor_reserved1: If provided and include_vendor_reserved1=True, use this value
                                 instead of device.node_thing_name
        capabilities: Optional list of capability names to enable during confirm

    Returns:
        dict with 'noc', 'matter_node_id', 'request_id', 'vendor_reserved1' on success, error string on failure
    """
    # Step 1: Initiate
    request_id, challenge = do_initiate(user, group_id)
    if request_id is None:
        return challenge  # This is the error string

    # Step 2: Build NOCSRElements with challenge as CSRNonce
    # The challenge is a 64-char hex string representing 32 bytes
    csr_nonce = bytes.fromhex(challenge)

    # Generate device CSR (DER format for TLV)
    _, csr_der = device.generate_csr()

    # Build NOCSRElements with or without vendor_reserved1
    if include_vendor_reserved1:
        vendor_reserved1_value = custom_vendor_reserved1 if custom_vendor_reserved1 else device.node_thing_name
        vendor_reserved1 = vendor_reserved1_value.encode('utf-8')
    else:
        vendor_reserved1 = None
        vendor_reserved1_value = None
    nocsr_elements = build_nocsr_elements_tlv(csr_der, csr_nonce, vendor_reserved1)

    # Step 3: Generate attestation challenge and sign
    attestation_challenge = os.urandom(16)

    if use_device_key:
        # Sign with device's registered private key
        attestation_signature = device.sign_matter_attestation(nocsr_elements, attestation_challenge)
        if attestation_signature is None:
            return "ERROR_SIGNING_FAILED"
    else:
        # Sign with random key (for pure Matter nodes or testing signature verification failure)
        random_key = ec.generate_private_key(ec.SECP256R1())
        attestation_signature = sign_attestation_data(nocsr_elements, attestation_challenge, random_key)

    # Step 4: Verify
    verify_result, error = do_verify_with_nocsr_elements(
        user, group_id, request_id, nocsr_elements, attestation_challenge, attestation_signature
    )
    if error:
        return error

    # Step 5: Confirm
    confirm_result = do_confirm(user, group_id, request_id, capabilities)
    if confirm_result is not True:
        return confirm_result

    return {
        "noc": verify_result.get("noc"),
        "matter_node_id": verify_result.get("matter_node_id"),
        "node_id": verify_result.get("node_id"),
        "request_id": request_id,
        "vendor_reserved1": vendor_reserved1_value
    }
