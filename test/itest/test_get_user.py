# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Integration tests for GET /v1/users/{userId}.

The endpoint resolves `me` to the authenticated caller and accepts the
caller's own user_id literally. Other user_ids return 403.
"""
import json

import requests

from test.itest.conftest import USER_API_GATEWAY_URL


def _assert_profile_matches(payload, expected_user_id, expected_email):
    """Compare the whole response payload against the expected dict."""
    expected = {
        "user_id": expected_user_id,
        "email": expected_email,
    }
    # phone_number is optional — only include it if the server returned one.
    if "phone_number" in payload:
        expected["phone_number"] = payload["phone_number"]

    assert payload == expected, f"response mismatch: {payload!r} vs {expected!r}"


def test_get_user_me_returns_caller_profile(test_user1):
    response = test_user1.get_user_details("me")
    assert response.status_code == 200, response.text
    _assert_profile_matches(response.json(), test_user1.sub, test_user1.username)

def test_get_user_unauthenticated_returns_401_or_403():
    """No auth header at all — API Gateway rejects before the lambda runs."""
    response = requests.get(f"{USER_API_GATEWAY_URL}/v1/users/me")
    # API Gateway returns 401 when a Cognito authorizer is configured and
    # the token is missing/invalid; some configurations surface 403 for the
    # same case. Accept either.
    assert response.status_code in (401, 403), (
        f"expected 401/403, got {response.status_code}: {response.text}"
    )


def test_get_user_by_foreign_id_denied(two_tenants):
    """GET /v1/users/{userId} must only serve 'me' or the caller's own id."""
    tenant_a, tenant_b = two_tenants
    user_a, user_b = tenant_a["user"], tenant_b["user"]

    foreign_id = user_b.sub
    # skip_cors_check: the OPTIONS preflight returns 403 here and would mask the
    # GET authorization behaviour under test.
    resp = user_a.make_api_request("GET", f"/v1/users/{foreign_id}", skip_cors_check=True)
    if resp.status_code in (200, 201):
        body = resp.json()
        returned_id = body.get("user_id") or body.get("sub") or body.get("id")
        assert returned_id != foreign_id, (
            "GET /v1/users/{foreign_id} returned another user's profile (IDOR)"
        )
    else:
        assert resp.status_code in (401, 403, 404), \
            f"unexpected status {resp.status_code}: {resp.text}"


def test_get_user_with_id_token_denied(test_user1):
    """An ID token must not authenticate this endpoint.

    Two independent guards produce this 401, and the test does not care which
    one fires: API Gateway validates the access token because the method
    declares authorizationScopes (an ID token has no `scope` claim), and the
    handler resolves the caller through cognito-idp:GetUser, which is
    access-token-only. Either alone is sufficient; both are deliberate.
    """
    # self.token is the id_token; self.access_token is the access token.
    resp = test_user1.make_api_request_with_token(
        "GET", "/v1/users/me",
        api_url=test_user1.user_api_gateway_url,
        skip_cors_check=True,
        token=test_user1.token,
    )
    assert resp.status_code == 401, (
        f"ID token was accepted on GET /v1/users/me: {resp.status_code} {resp.text}"
    )
