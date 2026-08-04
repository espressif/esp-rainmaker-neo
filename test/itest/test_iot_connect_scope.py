# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Live checks for the vended iot:Connect scope (assume_role fail-closed fix).

POST /v1/assumed-roles vends an MQTT session policy that scopes iot:Connect to
the caller's own per-session client id. These tests assert, against a live
deployment, that the policy:

  - ALLOWS  the caller's own per-session id : "user:<email>:<session>"
  - REJECTS the bare legacy id              : "user:<email>"
  - REJECTS another user's id               : "user:<other-email>:<session>"

The ':' delimiter cannot appear in an email/E.164 number, so the wildcard
"client/user:<email>:*" can only ever match the caller's own sessions. Run
against the ap-south-1 test backend carrying the fail-closed policy.
"""
import random
import string
import time

import pytest
from awscrt import auth
from awsiot import mqtt_connection_builder

from py_sdk.test_group import Group


def _rand_session(n=6):
    return "".join(random.choices(string.digits, k=n))


@pytest.fixture
def user_with_group(test_user1):
    """A user that owns one group, so the vended policy's topic statements have
    non-empty Resource lists (STS rejects a policy with an empty Resource)."""
    group_api = Group(test_user1)
    group_id = group_api.create_group("iot-connect-scope-test")
    try:
        yield test_user1
    finally:
        group_api.delete_group(group_id, warn_error=True)


def _try_connect(user, client_id, timeout=15):
    """Assume role as `user`, then attempt an MQTT-over-WSS connect as `client_id`.

    Returns (ok, detail): ok=True if the broker accepted the CONNECT. The
    connection is always torn down before returning.
    """
    creds = user.assume_role()
    assert creds is not None, "assume_role returned no credentials"

    provider = auth.AwsCredentialsProvider.new_static(
        access_key_id=creds["access_key"],
        secret_access_key=creds["secret_key"],
        session_token=creds["session_token"],
    )
    conn = mqtt_connection_builder.websockets_with_default_aws_signing(
        endpoint=user.iot_endpoint,
        region=user.region,
        credentials_provider=provider,
        client_id=client_id,
        clean_session=True,
        keep_alive_secs=30,
    )
    print(f"[iot-scope] connect as '{client_id}' ...")
    try:
        conn.connect().result(timeout=timeout)
    except Exception as e:  # denied CONNECT surfaces as a hangup/timeout here
        detail = f"{type(e).__name__}: {e}"
        print(f"[iot-scope] REJECTED: {detail}")
        return False, detail
    print("[iot-scope] ACCEPTED")
    try:
        conn.disconnect().result(timeout=timeout)
    except Exception:
        pass
    return True, "connected"


def test_own_per_session_client_id_allowed(user_with_group):
    client_id = f"user:{user_with_group.username}:{_rand_session()}"
    ok, detail = _try_connect(user_with_group, client_id)
    assert ok, f"own per-session id should connect, got: {detail}"


def test_bare_client_id_rejected(user_with_group):
    # The legacy bare id is no longer granted — must be refused (fail-closed).
    client_id = f"user:{user_with_group.username}"
    ok, detail = _try_connect(user_with_group, client_id)
    assert not ok, "bare legacy id must be rejected after the fail-closed fix"


def test_foreign_client_id_rejected(user_with_group, test_user2):
    # user1's own credentials, but claiming user2's client id — the cross-tenant
    # session-kick the fix closes. Must be refused.
    client_id = f"user:{test_user2.username}:{_rand_session()}"
    ok, detail = _try_connect(user_with_group, client_id)
    assert not ok, "connecting as another user's id must be rejected"


def _open_session(user, client_id, creds, timeout=15):
    """Open and hold an MQTT session as `client_id`. Returns (conn, state);
    state['interrupted'] flips True if the broker later drops the connection."""
    provider = auth.AwsCredentialsProvider.new_static(
        access_key_id=creds["access_key"],
        secret_access_key=creds["secret_key"],
        session_token=creds["session_token"],
    )
    state = {"interrupted": False}

    def on_interrupted(connection, error, **kwargs):
        state["interrupted"] = True
        print(f"[iot-scope] '{client_id}' interrupted: {error}")

    conn = mqtt_connection_builder.websockets_with_default_aws_signing(
        endpoint=user.iot_endpoint,
        region=user.region,
        credentials_provider=provider,
        on_connection_interrupted=on_interrupted,
        client_id=client_id,
        clean_session=True,
        keep_alive_secs=30,
    )
    conn.connect().result(timeout=timeout)
    print(f"[iot-scope] holding session '{client_id}'")
    return conn, state


def test_two_sessions_same_user_coexist(user_with_group):
    # Two concurrent sessions for the SAME user with DISTINCT suffixes must
    # both stay connected — this is the phone + dashboard coexistence claim.
    user = user_with_group
    creds = user.assume_role()
    assert creds is not None
    a_suf, b_suf = _rand_session(), _rand_session()
    while b_suf == a_suf:
        b_suf = _rand_session()
    a_id = f"user:{user.username}:{a_suf}"
    b_id = f"user:{user.username}:{b_suf}"

    conn_a, state_a = _open_session(user, a_id, creds)
    try:
        conn_b, state_b = _open_session(user, b_id, creds)
        try:
            time.sleep(5)  # give any kick time to land
            assert not state_a["interrupted"], "session A was kicked when B connected — suffixes collided"
            assert not state_b["interrupted"], "session B dropped unexpectedly"
        finally:
            conn_b.disconnect().result(timeout=15)
    finally:
        conn_a.disconnect().result(timeout=15)


def test_two_sessions_same_client_id_collide(user_with_group):
    # Negative control: identical client ids DO kick each other, which is
    # exactly what the per-session suffix above prevents.
    user = user_with_group
    creds = user.assume_role()
    assert creds is not None
    same_id = f"user:{user.username}:{_rand_session()}"

    conn_a, state_a = _open_session(user, same_id, creds)
    conn_b = None
    try:
        conn_b, _ = _open_session(user, same_id, creds)
        time.sleep(5)
        assert state_a["interrupted"], "first session should be kicked by a duplicate client id"
    finally:
        for conn in (conn_b, conn_a):
            if conn is not None:
                try:
                    conn.disconnect().result(timeout=15)
                except Exception:
                    pass
