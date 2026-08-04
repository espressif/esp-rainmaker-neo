# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Tenant-identity takeover via a user-writable custom:user_id.

custom:user_id is the tenant key on both Cognito pools (and custom:super_admin is the privilege
key on the admin pool). Both attributes are declared mutable, so the ONLY thing stopping a
signed-in principal from rewriting their own via Cognito's UpdateUserAttributes is a
write_attributes restriction on the app client that pins writable fields to standard profile
attributes. These tests assert that restriction is in place on every app client, at the
infrastructure level (no live user needed).

Coverage is split by what is reachable. An end user never holds a Cognito token — the issuer brokers
federation and password auth server-side and mints ours — so for the end-user pool the restriction is
verified as deployed configuration. An admin signs in to Cognito directly and holds a real Cognito
access token, so for the admin pool the attack is driven for real.
"""
import pytest
import boto3

from test.itest.conftest import (
    REGION,
    END_USER_POOL_ID,
    ADMIN_USER_POOL_ID,
)

_PROTECTED_CLIENTS = [
    (END_USER_POOL_ID, "espuser-idp-broker", ()),
    (ADMIN_USER_POOL_ID, "admin-client", ("custom:super_admin",)),
]


def _write_attributes_for(pool_id: str, client_name: str):
    """Return (client_id, write_attributes) for the named client, or (None, None) if absent.

    write_attributes is None when the client has no restriction — the vulnerable configuration
    (Cognito omits WriteAttributes entirely, meaning every mutable attribute is writable)."""
    cognito = boto3.client("cognito-idp", region_name=REGION)
    paginator = cognito.get_paginator("list_user_pool_clients")
    for page in paginator.paginate(UserPoolId=pool_id):
        for c in page["UserPoolClients"]:
            if c["ClientName"] == client_name:
                resp = cognito.describe_user_pool_client(UserPoolId=pool_id, ClientId=c["ClientId"])
                return c["ClientId"], resp["UserPoolClient"].get("WriteAttributes")
    return None, None


@pytest.mark.parametrize("pool_id,client_name,extra_forbidden", _PROTECTED_CLIENTS,
                         ids=[c[1] for c in _PROTECTED_CLIENTS])
def test_client_cannot_write_identity_attributes(pool_id, client_name, extra_forbidden):
    """Every app client must pin write_attributes so a principal's own token can never write
    custom:user_id (the tenant key) — nor custom:super_admin on the admin pool."""
    if not pool_id:
        pytest.skip(f"pool id for {client_name} not configured")

    client_id, write_attrs = _write_attributes_for(pool_id, client_name)
    if client_id is None:
        pytest.skip(f"client {client_name!r} not found on pool {pool_id}")

    assert write_attrs is not None, (
        f"{client_name} has no write_attributes restriction; custom:user_id is implicitly "
        f"writable — a principal can overwrite their tenant key (cross-tenant takeover)"
    )
    for forbidden in ("custom:user_id",) + tuple(extra_forbidden):
        assert forbidden not in write_attrs, (
            f"{forbidden} is listed in {client_name} write_attributes — a principal can still "
            f"overwrite it via UpdateUserAttributes"
        )


@pytest.mark.xdist_group("env_mut")
def test_admin_cannot_escalate_via_update_user_attributes(super_admin_user):
    """An admin holds a real Cognito access token, so it can call UpdateUserAttributes on itself.

    Writing custom:super_admin would be self-granted privilege, and custom:user_id would move the
    admin onto another tenant. The app client's write_attributes must refuse both. This is the one
    pool where the attack is reachable, so it is driven rather than inferred from configuration.
    """
    if not ADMIN_USER_POOL_ID:
        pytest.skip("admin pool id not configured")
    if not super_admin_user.access_token:
        pytest.skip("admin access token unavailable")

    cognito = boto3.client("cognito-idp", region_name=REGION)
    for attribute, value in (("custom:super_admin", "true"), ("custom:user_id", "some-other-tenant")):
        with pytest.raises(Exception) as excinfo:  # noqa: PT011 — Cognito's error type varies
            cognito.update_user_attributes(
                AccessToken=super_admin_user.access_token,
                UserAttributes=[{"Name": attribute, "Value": value}],
            )
        assert "NotAuthorized" in str(excinfo.value) or "not authorized" in str(excinfo.value).lower() \
            or "InvalidParameter" in str(excinfo.value), \
            f"writing {attribute} must be refused, got: {excinfo.value}"

    # The stored attribute is unchanged, so nothing partially applied.
    fresh = cognito.get_user(AccessToken=super_admin_user.access_token)
    stored = {a["Name"]: a["Value"] for a in fresh["UserAttributes"]}
    assert stored.get("custom:user_id") == super_admin_user.sub, \
        f"custom:user_id must still be the admin's own: {stored.get('custom:user_id')}"
