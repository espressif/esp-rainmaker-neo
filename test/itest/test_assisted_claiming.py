# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Integration tests for assisted claiming.

These exercise the one thing unit tests cannot: that a certificate minted by the
claiming CA is actually usable. Everything else — the certificate profile, the
entitlement checks, the re-claim semantics — is pinned by unit tests against
mocks. What only a deployed environment can prove is that a device holding the
issued certificate connects to AWS IoT and is recognised as its node.

Skipped automatically when the deployment does not have claiming enabled, so
the suite stays green wherever claiming is switched off.
"""
import calendar
import json
import os
import time
import uuid

import boto3
import pytest
from cryptography import x509
from cryptography.hazmat.primitives.asymmetric import ec

from py_sdk.test_device import Device
from py_sdk.test_group import Group
from py_sdk.test_user import User
from test.itest.conftest import CA_CERT, DEBUG, IOT_ENDPOINT, REGION, connect_device_with_retry

# Claiming configuration the suite bootstraps with, via the superadmin admin API
# (§3.9). Shared by the bootstrap fixture and the tests that assert the
# configured identity reaches issued certificates. `mode` is part of this
# document now — it is what activates claiming at runtime (the claim group
# deploys inert), so the bootstrap must set it or every test skips as
# "claiming not available".
CLAIM_CONFIG = {
    "mode": "user_authenticated",
    "subject": {
        "country": "IN",
        "state": "Maharashtra",
        "locality": "Pune",
        "organization": "Espressif Systems",
        "organizational_unit": "RainMaker",
        "email": "rainmaker@espressif.com",
    },
    "ca_validity_years": 30,
    "leaf_validity_years": 10,
}


# ---------------------------------------------------------------- helpers


def _random_mac():
    """A MAC unlikely to collide with another run's reservation.

    Reservations are never deleted — the quota is a lifetime cap — so every
    claim here permanently consumes one of the caller's slots. A pytest session
    gets a fresh user, so the budget that matters is per run, not per user
    lifetime: keep the whole file comfortably under DefaultMaxNodesPerClaimant
    (20). Current cost is roughly a dozen; adding a test that claims several
    nodes is the thing most likely to push it over.
    """
    return "AA" + uuid.uuid4().hex[:10].upper()


# The claim flow itself lives in the SDK (User.claim_initiate / claim_verify /
# generate_claim_csr / claim) so the CLI and these tests share one
# implementation. The thin wrappers below keep the tests' call sites readable.


def _initiate(user, mac_addr, skip_cors_check=False):
    return user.claim_initiate(mac_addr, skip_cors_check=skip_cors_check)


def _verify(user, mac_addr, csr_pem, capabilities=None):
    return user.claim_verify(mac_addr, csr_pem, capabilities=capabilities)


def _make_csr(common_name="ignored-by-the-server"):
    """A P-256 CSR whose subject the server is required to discard."""
    return User.generate_claim_csr(common_name)


def _claim(user, mac_addr=None):
    """Run a full claim and return (node_id, cert_pem, ca_pem, key_pem)."""
    c = user.claim(mac_addr)
    return c["node_id"], c["certificate"], c["ca_certificate"], c["private_key"]


def _not_after(cert):
    """Expiry, compatible across cryptography versions.

    not_valid_after_utc arrived in cryptography 42; not_valid_after is
    deprecated in newer releases. Using the same accessor for both operands
    keeps the comparison between naive and aware datetimes consistent.
    """
    return getattr(cert, "not_valid_after_utc", None) or cert.not_valid_after


def _cleanup_node(node_id):
    """Detach, deactivate, and delete a claimed node's IoT resources.

    Claimed nodes are real Things. The reservation itself is deliberately not
    removable, so the node ID stays spent — only the IoT side is cleaned up.
    """
    iot = boto3.client("iot", region_name=REGION)
    try:
        principals = iot.list_thing_principals(thingName=node_id).get("principals", [])
    except iot.exceptions.ResourceNotFoundException:
        return
    for arn in principals:
        cert_id = arn.split("/")[-1]
        try:
            iot.detach_thing_principal(thingName=node_id, principal=arn)
            iot.update_certificate(certificateId=cert_id, newStatus="INACTIVE")
            iot.delete_certificate(certificateId=cert_id, forceDelete=True)
        except Exception:  # noqa: BLE001 - best-effort teardown
            pass
    try:
        iot.delete_thing(thingName=node_id)
    except Exception:  # noqa: BLE001
        pass


@pytest.fixture
def claimed_nodes():
    """Track claimed node IDs and tear down their IoT resources afterwards."""
    created = []
    yield created
    for node_id in created:
        _cleanup_node(node_id)


@pytest.fixture(scope="session", autouse=True)
def _bootstrap_claiming_ca():
    """Stand up the claiming CA before the suite runs.

    The CA is minted at runtime through the superadmin admin API (§3.9), not at
    deploy time, so the suite bootstraps it itself: set the certificate
    configuration, then mint. Idempotent — a repeat mint is a no-op — and
    tolerant of a claiming-disabled deployment, where the per-test skip below
    takes over and the mint call is simply a no-op we ignore.

    Both calls skip the CORS preflight check. When the claim group is not
    deployed the admin routes do not exist, and the preflight assertion inside
    the SDK would fail the *fixture* — turning what should be a clean skip for
    the whole file into a setup error on every test, before the per-test
    availability probe below ever runs. The preflight on these two routes is
    still asserted, on a deployment that has them, by
    test_admin_config_round_trips_and_mint_is_idempotent.
    """
    from test.itest.conftest import admin_user_pool

    admin = admin_user_pool.acquire()
    try:
        cfg = admin.claim_admin_set_config(CLAIM_CONFIG, skip_cors_check=True)
        if cfg.status_code != 200:
            print(f"[claiming] config not applied ({cfg.status_code}): {cfg.text}")
        resp = admin.claim_admin_mint_ca(skip_cors_check=True)
        if resp.status_code not in (200, 201):
            print(f"[claiming] CA bootstrap not applied ({resp.status_code}): {resp.text}")
    finally:
        admin_user_pool.release(admin)
    yield


@pytest.fixture(autouse=True)
def _skip_when_claiming_disabled(test_user1):
    """Claiming is an optional, separately-deployed stack group.

    Probes with a deliberately invalid MAC so the check costs no quota; an
    enabled deployment rejects the input with 400. Two "not available" shapes
    are skipped:
      - 404: the claim group is deployed but claiming is inactive (the handler
        fails closed on an unconfigured variant);
      - 403: the claim routes are absent entirely (the claim group is not
        deployed), so API Gateway rejects the unknown path with "Missing
        Authentication Token".
    Probing with a real MAC would mint a reservation per test, and since
    reservations are never released that would consume the caller's lifetime
    allowance just to ask whether the feature exists.

    The probe skips the CORS preflight for the same reason the CA bootstrap
    above does: the 403-means-absent case is exactly the one where the
    preflight assertion would fire first and turn the skip into an error. Every
    other call in this file keeps the check, so the claim routes' preflight
    stays covered on a deployment that has them.
    """
    probe = _initiate(test_user1, "not-a-mac", skip_cors_check=True)
    if probe.status_code in (403, 404):
        pytest.skip("assisted claiming is not available on this deployment")
    assert probe.status_code == 400, (
        f"expected 400 for a malformed MAC on an enabled deployment, got "
        f"{probe.status_code}: {probe.text}"
    )


# ---------------------------------------------------------------- tests


def test_claimed_certificate_connects_to_mqtt(test_user1, claimed_nodes):
    """The assertion no unit test can make: the issued certificate works.

    Everything upstream can be correct — profile, chain, binding — and the
    device still fail to connect if the certificate is not registered, not
    attached, or the client ID does not match the Thing.
    """
    node_id, cert_pem, _ca_pem, key_pem = _claim(test_user1)
    claimed_nodes.append(node_id)

    device = Device(node_id, key_pem, cert_pem, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
    assert device.mqtt_connect(), "device could not connect with its claimed certificate"
    device.disconnect()


def test_claimed_certificate_authenticates_the_user_node_mapping(test_user1, claimed_nodes):
    """The same certificate, used the other way.

    MQTT exercises the key for TLS client auth. Establishing the primary
    user-node mapping exercises it for challenge-response: the device signs a
    server-issued challenge and the backend verifies the signature against the
    certificate registered for that node. Both must work off one claimed
    certificate: a claimed node that cannot complete the mapping has no owner,
    so nothing downstream that keys on ownership can reach it.
    """
    node_id, cert_pem, _ca_pem, key_pem = _claim(test_user1)
    claimed_nodes.append(node_id)

    device = Device(node_id, key_pem, cert_pem, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
    assert connect_device_with_retry(device, max_retries=3, base_delay=2), \
        "claimed device could not connect"
    device.clear_queues()
    device.clear_callbacks()

    group_id = Group(test_user1).create_group(f"claimed-{uuid.uuid4().hex[:8]}")
    err = test_user1.do_user_node_assoc(device, group_id)
    assert err is None, f"challenge-response association failed: {err}"

    assert device.wait_for_group_info(), "device never received group info"
    assert device.group_id == group_id
    device.disconnect()


def test_certificate_names_the_reserved_node_not_the_csr_subject(test_user1, claimed_nodes):
    """Identity is server-determined: the CSR subject must be discarded."""
    mac = _random_mac()
    node_id = _initiate(test_user1, mac).json()["node_id"]
    claimed_nodes.append(node_id)

    csr_pem, _ = _make_csr(common_name="attacker-chosen-name")
    resp = _verify(test_user1, mac, csr_pem)
    assert resp.status_code == 201, resp.text

    cert = x509.load_pem_x509_certificate(resp.json()["certificate"].encode())
    cn = cert.subject.get_attributes_for_oid(x509.oid.NameOID.COMMON_NAME)[0].value
    assert cn == node_id
    assert cn != "attacker-chosen-name"


def test_certificate_profile_matches_the_spec(test_user1, claimed_nodes):
    node_id, cert_pem, ca_pem, _ = _claim(test_user1)
    claimed_nodes.append(node_id)
    cert = x509.load_pem_x509_certificate(cert_pem.encode())

    assert isinstance(cert.public_key(), ec.EllipticCurvePublicKey)
    assert cert.public_key().curve.name == "secp256r1"

    bc = cert.extensions.get_extension_for_class(x509.BasicConstraints)
    assert bc.critical and bc.value.ca is False

    ku = cert.extensions.get_extension_for_class(x509.KeyUsage)
    assert ku.critical and ku.value.digital_signature

    # The omission that lets one certificate serve as both the IoT client
    # certificate and a Matter attestation certificate.
    with pytest.raises(x509.ExtensionNotFound):
        cert.extensions.get_extension_for_class(x509.ExtendedKeyUsage)

    cert.extensions.get_extension_for_class(x509.SubjectKeyIdentifier)
    cert.extensions.get_extension_for_class(x509.AuthorityKeyIdentifier)

    assert len(cert.serial_number.to_bytes((cert.serial_number.bit_length() + 7) // 8, "big")) <= 20

    # Issued by the deployment's CA, and that CA outlives the leaf.
    ca = x509.load_pem_x509_certificate(ca_pem.encode())
    assert cert.issuer == ca.subject
    assert _not_after(ca) >= _not_after(cert)


def test_initiate_is_idempotent_and_normalizes_the_mac(test_user1, claimed_nodes):
    """One physical device is one reservation, however its MAC is spelled."""
    mac = _random_mac()
    first = _initiate(test_user1, mac)
    assert first.status_code == 201, first.text
    node_id = first.json()["node_id"]
    claimed_nodes.append(node_id)

    repeat = _initiate(test_user1, mac)
    assert repeat.status_code == 200
    assert repeat.json()["node_id"] == node_id

    # Colon-separated spelling of the same address.
    spaced = ":".join(mac[i:i + 2] for i in range(0, len(mac), 2))
    other = _initiate(test_user1, spaced.lower())
    assert other.status_code == 200, other.text
    assert other.json()["node_id"] == node_id


def test_reclaim_replaces_the_certificate_and_keeps_the_node(test_user1, claimed_nodes):
    """The flash-erase path: same node, new certificate, old one retired."""
    mac = _random_mac()
    node_id, first_cert, _, _ = _claim(test_user1, mac)
    claimed_nodes.append(node_id)

    csr_pem, key_pem = _make_csr()
    resp = _verify(test_user1, mac, csr_pem)
    assert resp.status_code == 201, resp.text
    assert resp.json()["node_id"] == node_id, "re-claim must not mint a new node"
    second_cert = resp.json()["certificate"]
    assert second_cert != first_cert

    iot = boto3.client("iot", region_name=REGION)
    principals = iot.list_thing_principals(thingName=node_id)["principals"]
    assert len(principals) == 1, "exactly one certificate should remain attached"

    # The replacement is what the device must now use.
    device = Device(node_id, key_pem, second_cert, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
    assert device.mqtt_connect(), "device could not connect with its replacement certificate"
    device.disconnect()


def test_reclaim_invalidates_the_previous_certificate(test_user1, claimed_nodes):
    """Replacement must actually revoke, not merely add.

    Re-claim detaches and deactivates the prior certificate. If deactivation
    did not take, a device flashed with a superseded certificate — or anyone
    holding a copy of it — would keep full access to the node indefinitely.
    That is the whole security value of replacing on re-claim, and it can only
    be proven against a live broker.
    """
    mac = _random_mac()
    node_id, old_cert, _, old_key = _claim(test_user1, mac)
    claimed_nodes.append(node_id)

    old_device = Device(node_id, old_key, old_cert, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
    assert old_device.mqtt_connect(), "sanity: the first certificate should work"
    old_device.disconnect()

    csr_pem, new_key = _make_csr()
    resp = _verify(test_user1, mac, csr_pem)
    assert resp.status_code == 201, resp.text
    new_cert = resp.json()["certificate"]

    # The replacement works.
    new_device = Device(node_id, new_key, new_cert, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
    assert new_device.mqtt_connect(), "replacement certificate should work"
    new_device.disconnect()

    # The superseded one must not. Allow a little time for deactivation to
    # propagate rather than asserting on the first attempt.
    for attempt in range(6):
        stale = Device(node_id, old_key, old_cert, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
        if not stale.mqtt_connect():
            break
        stale.disconnect()
        time.sleep(5)
    else:
        pytest.fail("the superseded certificate still connects — replacement did not revoke it")


def test_a_user_cannot_act_as_another_users_node(test_user1, test_user2, claimed_nodes):
    """Isolation at the transport layer, not just in the claim path.

    Two users claiming the same MAC get separate nodes, so there is no way to
    ask for someone else's node ID. This checks the next move: taking a
    legitimately issued certificate and presenting it as the other user's node.
    The IoT policy binds the client ID to the Thing the certificate is attached
    to, so it must be refused.
    """
    mac = _random_mac()

    victim_id, _, _, _ = _claim(test_user1, mac)
    claimed_nodes.append(victim_id)

    # Same MAC, different caller — a distinct node, never the victim's.
    attacker_id, attacker_cert, _, attacker_key = _claim(test_user2, mac)
    claimed_nodes.append(attacker_id)
    assert attacker_id != victim_id, "a second caller must never resolve to the first caller's node"

    # The attacker's own certificate works as their own node.
    own = Device(attacker_id, attacker_key, attacker_cert, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
    assert own.mqtt_connect(), "sanity: the attacker's certificate should work as their own node"
    own.disconnect()

    # ...but not as the victim's node.
    impostor = Device(victim_id, attacker_key, attacker_cert, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
    assert not impostor.mqtt_connect(), \
        "a certificate was accepted for a node it is not attached to"


def test_every_user_and_mac_pair_gets_its_own_node(test_user1, test_user2, claimed_nodes):
    """Unique {user, MAC} pairs must never share a node ID."""
    mac_a, mac_b = _random_mac(), _random_mac()
    seen = {}
    for user, label in ((test_user1, "u1"), (test_user2, "u2")):
        for mac in (mac_a, mac_b):
            resp = _initiate(user, mac)
            assert resp.status_code == 201, resp.text
            node_id = resp.json()["node_id"]
            claimed_nodes.append(node_id)
            seen[(label, mac)] = node_id

    assert len(set(seen.values())) == 4, f"node IDs collided across user/MAC pairs: {seen}"


def test_verify_without_a_claim_is_refused(test_user1):
    """No reservation, no certificate — and no IoT resources created."""
    csr_pem, _ = _make_csr()
    resp = _verify(test_user1, _random_mac(), csr_pem)
    assert resp.status_code == 403, resp.text
    assert "not claimed" in resp.json().get("message", "").lower()


def test_provenance_tags_identify_the_claim_and_the_claimant(test_user1, claimed_nodes):
    """Claimed nodes must be findable by the same fleet-index filters as
    dashboard-registered ones, and must not be able to pose as one."""
    node_id, _, _, _ = _claim(test_user1)
    claimed_nodes.append(node_id)

    me = test_user1.get_user_details("me")
    assert me.status_code == 200, me.text
    user_id = json.loads(me.text)["user_id"]

    iot_data = boto3.client(
        "iot-data", region_name=REGION, endpoint_url=f"https://{IOT_ENDPOINT}"
    )
    shadow = iot_data.get_thing_shadow(thingName=node_id, shadowName="iparams")
    payload = shadow["payload"].read().decode()

    assert "registered_from" in payload
    assert "claim" in payload
    assert "created_by" in payload
    assert user_id in payload
    assert "dashboard" not in payload


def test_admin_can_see_who_claimed_a_node_and_when(test_user1, claimed_nodes):
    """Provenance has to be visible to an administrator, not merely recorded.

    reg_ts and admin_id are written to the node row, but the console searches
    the fleet index, which reads shadows — and iot:DescribeThing carries no
    creation date at all. Only the tags make any of this reachable.
    """
    before = time.time()
    node_id, _, _, _ = _claim(test_user1)
    claimed_nodes.append(node_id)

    me = test_user1.get_user_details("me")
    user_id = json.loads(me.text)["user_id"]

    iot_data = boto3.client(
        "iot-data", region_name=REGION, endpoint_url=f"https://{IOT_ENDPOINT}"
    )
    shadow = json.loads(
        iot_data.get_thing_shadow(thingName=node_id, shadowName="iparams")["payload"].read()
    )
    tags = shadow["state"]["reported"]["data"]["admin"]["t"]

    assert tags["registered_from"] == "claim"
    assert tags["created_by"] == user_id

    # timegm, not mktime: the tag is UTC and mktime would read the parsed
    # struct as local time, skewing the comparison by the machine's offset.
    stamped = calendar.timegm(time.strptime(tags["registered_at"], "%Y-%m-%dT%H:%M:%SZ"))
    # Generous window: only checking it is a real, current timestamp.
    assert abs(stamped - before) < 3600, f"registered_at {tags['registered_at']} is not recent"


def test_registered_at_survives_a_reclaim(test_user1, claimed_nodes):
    """It must mean "entered the fleet", not "last claimed"."""
    mac = _random_mac()
    node_id, _, _, _ = _claim(test_user1, mac)
    claimed_nodes.append(node_id)

    iot_data = boto3.client(
        "iot-data", region_name=REGION, endpoint_url=f"https://{IOT_ENDPOINT}"
    )

    def _registered_at():
        payload = iot_data.get_thing_shadow(thingName=node_id, shadowName="iparams")["payload"].read()
        return json.loads(payload)["state"]["reported"]["data"]["admin"]["t"]["registered_at"]

    original = _registered_at()
    time.sleep(2)

    csr_pem, _ = _make_csr()
    assert _verify(test_user1, mac, csr_pem).status_code == 201
    assert _registered_at() == original, "re-claim must not overwrite the registration time"


@pytest.mark.skipif(
    os.environ.get("RUN_CLAIM_QUOTA_TEST") != "1",
    reason=(
        "permanently consumes the pooled test user's lifetime claim quota — "
        "reservations are never deleted, so this cannot be undone. "
        "Set RUN_CLAIM_QUOTA_TEST=1 to run against a throwaway user."
    ),
)
def test_quota_is_enforced_and_is_a_lifetime_cap(test_user1, claimed_nodes):
    """Minting is bounded per caller, and repeat claims stay exempt."""
    limit = int(os.environ.get("CLAIM_QUOTA_LIMIT", "20"))

    first_mac = None
    rejected_at = None
    for i in range(limit + 5):
        mac = _random_mac()
        resp = _initiate(test_user1, mac)
        if resp.status_code == 403:
            rejected_at = i
            assert "quota" in resp.json().get("message", "").lower()
            break
        assert resp.status_code == 201, resp.text
        claimed_nodes.append(resp.json()["node_id"])
        if first_mac is None:
            first_mac = mac

    assert rejected_at is not None, f"quota was never enforced within {limit + 5} claims"

    # An already-reserved device still resolves at the limit, so a device that
    # is factory-erased after the quota fills can still be re-provisioned.
    repeat = _initiate(test_user1, first_mac)
    assert repeat.status_code == 200, repeat.text


@pytest.mark.parametrize(
    "body,expected",
    [
        ({"mac_addr": "not-a-mac", "csr": "x"}, 400),
        ({"mac_addr": "AABBCCDDEEFF"}, 400),
        ({"csr": "x"}, 400),
    ],
)
def test_verify_rejects_malformed_requests(test_user1, body, expected):
    resp = test_user1.make_api_request("POST", "/v1/claim/verify", data=json.dumps(body))
    assert resp.status_code == expected, resp.text


def _not_before(cert):
    """notBefore, compatible across cryptography versions (see _not_after)."""
    return getattr(cert, "not_valid_before_utc", None) or cert.not_valid_before


def test_admin_config_round_trips_and_mint_is_idempotent(super_admin_user, test_user1):
    """The superadmin config + mint API: a non-admin is refused, config
    round-trips, and a repeat mint is a no-op against the already-bootstrapped CA."""
    # A regular authenticated user is refused at the admin gate.
    denied = test_user1.claim_admin_set_config(CLAIM_CONFIG)
    assert denied.status_code == 403, denied.text

    put = super_admin_user.claim_admin_set_config(CLAIM_CONFIG)
    assert put.status_code == 200, put.text

    got = super_admin_user.claim_admin_get_config()
    assert got.status_code == 200, got.text
    cfg = got.json()["config"]
    assert cfg["mode"] == "user_authenticated"
    assert cfg["subject"]["organization"] == "Espressif Systems"
    assert cfg["ca_validity_years"] == 30
    assert cfg["leaf_validity_years"] == 10

    # A recognized-but-unimplemented mode is refused at the config API.
    bad = super_admin_user.claim_admin_set_config({**CLAIM_CONFIG, "mode": "device_attested"})
    assert bad.status_code == 400, bad.text

    # The session fixture already minted the CA, so this reports it unchanged.
    mint = super_admin_user.claim_admin_mint_ca()
    assert mint.status_code == 200, mint.text

    ca = super_admin_user.claim_admin_get_ca()
    assert ca.status_code == 200, ca.text
    assert "BEGIN CERTIFICATE" in ca.json()["ca_certificate"]


def test_leaf_carries_the_configured_subject_and_validity(test_user1, claimed_nodes):
    """The identity set through the admin config API reaches the issued leaf:
    CN is the node ID, the operator subject is applied, and the leaf lives for
    the configured term."""
    node_id, cert_pem, _ca, _ = _claim(test_user1)
    claimed_nodes.append(node_id)
    cert = x509.load_pem_x509_certificate(cert_pem.encode())

    def val(oid):
        attrs = cert.subject.get_attributes_for_oid(oid)
        return attrs[0].value if attrs else None

    assert val(x509.oid.NameOID.COMMON_NAME) == node_id
    assert val(x509.oid.NameOID.COUNTRY_NAME) == "IN"
    assert val(x509.oid.NameOID.ORGANIZATION_NAME) == "Espressif Systems"
    assert val(x509.oid.NameOID.ORGANIZATIONAL_UNIT_NAME) == "RainMaker"

    span_days = (_not_after(cert) - _not_before(cert)).days
    assert 10 * 365 - 3 <= span_days <= 10 * 365 + 3
