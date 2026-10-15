# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""A principal must never be able to rewrite the attribute that names their identity.

custom:user_id is the tenant key on the admin pool, and whatever claim a provider row maps to sub
names the ESP User id of the accounts that provider opens (federation.md, "What trust rests on"). custom:super_admin on the admin pool
grants nothing any more but must stay unwritable too. Cognito lets a signed-in principal rewrite
whatever its app client may write, so every client on such a pool, enumerated rather than named,
must set write_attributes and leave these out. The end-user pool this repo deploys declares no
custom:user_id; it is checked anyway, so a client added there later cannot open the same hole.

An admin holds a real Cognito access token, so on the admin pool the attack is driven for real.
"""
import re

import boto3
import pytest

from test.itest.conftest import (
    ADMIN_USER_POOL_ID,
    END_USER_POOL_ID,
    REGION,
)

_PROTECTED_POOLS = [
    (END_USER_POOL_ID, "end-user", ()),
    (ADMIN_USER_POOL_ID, "admin", ("custom:super_admin",)),
]

_PROVIDER_TABLE = "espuser-identity-providers"
_COGNITO_ISSUER = re.compile(r"^https://cognito-idp\.([a-z0-9-]+)\.amazonaws\.com/([\w-]+_[0-9a-zA-Z]+)$")


def _all_clients(pool_id: str, region: str):
    """Every app client on the pool, as (name, write_attributes); None means every attribute is writable."""
    cognito = boto3.client("cognito-idp", region_name=region)
    paginator = cognito.get_paginator("list_user_pool_clients")
    out = []
    for page in paginator.paginate(UserPoolId=pool_id):
        for c in page["UserPoolClients"]:
            resp = cognito.describe_user_pool_client(UserPoolId=pool_id, ClientId=c["ClientId"])
            out.append((c["ClientName"], resp["UserPoolClient"].get("WriteAttributes")))
    return out


def _write_offenders(pool_id: str, region: str, forbidden: tuple):
    """One line per app client on the pool that leaves any attribute in forbidden writable."""
    clients = _all_clients(pool_id, region)
    assert clients, f"no app clients found on pool {pool_id}"
    offenders = []
    for name, write_attrs in clients:
        if write_attrs is None:
            offenders.append(f"{name}: no write_attributes restriction (every attribute is writable)")
            continue
        offenders += [f"{name}: {attr} is writable" for attr in forbidden if attr in write_attrs]
    return offenders


def _sub_mapped_cognito_pools():
    """(provider_name, region, pool_id, claim) for every provider row that maps sub to a claim other than Cognito's own immutable sub."""
    table = boto3.resource("dynamodb", region_name=REGION).Table(_PROVIDER_TABLE)
    rows, scan_kwargs = [], {}
    while True:
        page = table.scan(**scan_kwargs)
        rows += page["Items"]
        if "LastEvaluatedKey" not in page:
            break
        scan_kwargs["ExclusiveStartKey"] = page["LastEvaluatedKey"]

    pools = []
    for row in rows:
        claim = (row.get("attribute_mapping") or {}).get("sub")
        if not claim or claim == "sub":
            continue
        match = _COGNITO_ISSUER.match(row.get("issuer", ""))
        assert match, (f"provider {row['provider_name']} maps sub to {claim} but its issuer is not a Cognito pool; "
                       f"confirm by hand that its users cannot set their own {claim}")
        pools.append((row["provider_name"], match.group(1), match.group(2), claim))
    return pools


@pytest.mark.parametrize("pool_id,pool_label,extra_forbidden", _PROTECTED_POOLS,
                         ids=[p[1] for p in _PROTECTED_POOLS])
def test_no_client_can_write_identity_attributes(pool_id, pool_label, extra_forbidden):
    """Every app client, not only the ones we deploy: one client added later by a console edit re-opens the hole for the whole pool."""
    if not pool_id:
        pytest.skip(f"pool id for {pool_label} not configured")

    offenders = _write_offenders(pool_id, REGION, ("custom:user_id",) + tuple(extra_forbidden))
    assert not offenders, (
        f"app clients on the {pool_label} pool let a principal rewrite their own identity via "
        f"UpdateUserAttributes:\n  " + "\n  ".join(offenders)
    )


def test_sub_mapped_pools_forbid_writing_the_mapped_claim():
    """The precondition for mapping sub, checked on every pool a provider row maps rather than only the pools this repo deploys."""
    pools = _sub_mapped_cognito_pools()
    if not pools:
        pytest.skip("no provider row maps sub to a writable claim")

    offenders = []
    for provider_name, region, pool_id, claim in pools:
        offenders += [f"{provider_name}: {o}" for o in _write_offenders(pool_id, region, (claim,))]
    assert not offenders, "a pool lets its users choose their ESP User id:\n  " + "\n  ".join(offenders)


@pytest.mark.xdist_group("env_mut")
def test_admin_cannot_escalate_via_update_user_attributes(admin_user):
    """An admin holds a real Cognito access token, so it can call UpdateUserAttributes on itself.

    Writing custom:user_id would move the admin onto another tenant; custom:super_admin grants
    nothing any more but must stay unwritable all the same, so a stale reader of the flag cannot be
    fed a self-set value. The app client's write_attributes must refuse both. This is the one
    pool where the attack is reachable, so it is driven rather than inferred from configuration.
    """
    if not ADMIN_USER_POOL_ID:
        pytest.skip("admin pool id not configured")
    if not admin_user.access_token:
        pytest.skip("admin access token unavailable")

    cognito = boto3.client("cognito-idp", region_name=REGION)
    for attribute, value in (("custom:super_admin", "true"), ("custom:user_id", "some-other-tenant")):
        with pytest.raises(Exception) as excinfo:  # noqa: PT011 — Cognito's error type varies
            cognito.update_user_attributes(
                AccessToken=admin_user.access_token,
                UserAttributes=[{"Name": attribute, "Value": value}],
            )
        assert "NotAuthorized" in str(excinfo.value) or "not authorized" in str(excinfo.value).lower() \
            or "InvalidParameter" in str(excinfo.value), \
            f"writing {attribute} must be refused, got: {excinfo.value}"

    # The stored attribute is unchanged, so nothing partially applied.
    fresh = cognito.get_user(AccessToken=admin_user.access_token)
    stored = {a["Name"]: a["Value"] for a in fresh["UserAttributes"]}
    assert stored.get("custom:user_id") == admin_user.sub, \
        f"custom:user_id must still be the admin's own: {stored.get('custom:user_id')}"
