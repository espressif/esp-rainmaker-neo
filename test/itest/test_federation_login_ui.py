# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Browser-level test of the brokered federation login (Cognito as upstream provider).

The other federation tests script the hosted UI with `requests`, which ignores SameSite and
hardcodes Cognito's form field names. A real browser exercises what those cannot: the full
redirect chain, and whether the esp_flow_id cookie actually survives the cross-site hop from
Cognito back to our callback. Spec: federation.md.

Requires playwright (declared in requirements.txt); the module skips cleanly without it.
"""
import uuid
from urllib.parse import urlparse, parse_qs

import pytest

from test.itest.conftest import (
    API_GATEWAY_URL,
    END_USER_POOL_ID,
    IDENTITY_POOL_ID,
    IOT_ENDPOINT,
    REGION,
    USER_API_GATEWAY_URL,
    decode_jwt_claims,
    pkce_pair,
)
from test.itest.email_utils import generate_random_email, generate_test_password
from py_sdk.test_user import User

pytest.importorskip("playwright.sync_api")

REDIRECT_URI = "https://example.com/itest-fed-ui-callback"


@pytest.fixture
def cognito_end_user(provision_end_user):
    """A password user in the provider's pool, removed afterwards by the shared factory."""
    email = generate_random_email() or f"fedui-{uuid.uuid4().hex[:10]}@example.com"
    password = generate_test_password()
    user = User(email, password, REGION, IDENTITY_POOL_ID,
                API_GATEWAY_URL, USER_API_GATEWAY_URL, IOT_ENDPOINT)
    user.end_user_pool_id = END_USER_POOL_ID
    provision_end_user(email, {"email": email, "email_verified": "true"}, password)
    return user, email, password


def test_federation_login_ui_browser_end_to_end(cognito_end_user, super_admin_user, chromium_browser):
    """Real browser: /oauth2/authorize -> Cognito hosted UI -> our callback -> redirect_uri?code=..."""
    if not USER_API_GATEWAY_URL or not END_USER_POOL_ID:
        pytest.skip("espuser outputs not configured")
    user, email, password = cognito_end_user

    client_id = "itest_fedui_" + uuid.uuid4().hex[:8]
    created = super_admin_user.create_oauth_client({
        "client_id": client_id, "client_name": "itest federation ui", "client_type": "public",
        "redirect_uris": [REDIRECT_URI], "grant_types": ["authorization_code", "refresh_token"],
        "scopes": ["openid", "email", "profile"], "require_pkce": True,
    })
    assert created.status_code == 201, created.text

    try:
        verifier, challenge = pkce_pair()
        authorize_url = (
            f"{USER_API_GATEWAY_URL}/oauth2/authorize?response_type=code"
            f"&client_id={client_id}&redirect_uri={REDIRECT_URI}"
            f"&scope=openid%20email%20profile&state=fedui123"
            f"&code_challenge={challenge}&code_challenge_method=S256"
        )

        context = chromium_browser.new_context()
        page = context.new_page()
        try:
            page.goto(authorize_url, wait_until="domcontentloaded")

            # Navigate the federation leg directly rather than picking a provider: with more than one
            # enabled, authorize serves the built-in login page and there is no chooser on it yet.
            page.goto(f"{USER_API_GATEWAY_URL}/oauth2/federation/start?provider=cognito",
                      wait_until="domcontentloaded")
            assert "amazoncognito.com" in page.url, \
                f"expected the upstream hosted UI, on {page.url}"

            # The cookie must be readable by our origin for the callback to resolve the flow.
            flow_cookie = [c for c in context.cookies() if c["name"] == "esp_flow_id"]
            assert flow_cookie, "authorize must set esp_flow_id before leaving for the upstream"
            assert flow_cookie[0]["sameSite"] in ("Lax", "None"), (
                f"esp_flow_id SameSite={flow_cookie[0]['sameSite']} would be dropped on the "
                "cross-site return from the upstream provider"
            )

            # The hosted page renders the sign-in form twice (desktop and mobile) with duplicate ids,
            # so pick the visible one rather than the first match.
            page.locator("#signInFormUsername").locator("visible=true").first.fill(email)
            page.locator("#signInFormPassword").locator("visible=true").first.fill(password)
            with page.expect_navigation(url=lambda u: u.startswith(REDIRECT_URI), timeout=30000):
                page.locator("input[name=signInSubmitButton]").locator("visible=true").first.click()

            landed_url = page.url
            assert "code=" in landed_url, f"redirect must carry the authorization code: {landed_url}"
            assert "state=fedui123" in landed_url, f"state must round-trip: {landed_url}"
        finally:
            context.close()

        auth_code = parse_qs(urlparse(landed_url).query)["code"][0]
        exchanged = user.oauth_exchange_code(auth_code, verifier, client_id, REDIRECT_URI)
        assert exchanged.status_code == 200, exchanged.text
        tokens = exchanged.json()
        assert tokens.get("access_token") and tokens.get("refresh_token")
        assert tokens.get("id_token"), "openid was requested, so id_token must be present"

        # Our own tokens, not the upstream's — the whole point of brokering.
        claims = decode_jwt_claims(tokens["access_token"])
        assert "cognito-idp" not in claims["iss"], \
            f"client must never receive a Cognito-issued token: iss={claims['iss']}"
        assert claims["aud"] == client_id, f"token must be audienced to our client: {claims}"

        userinfo = user.oauth_userinfo(tokens["access_token"])
        assert userinfo.status_code == 200, userinfo.text
        assert userinfo.json()["email"] == email

        # The refresh token the browser flow produced must rotate like any other, and the new access
        # token must still identify the same subject without going back to the provider.
        refreshed = user.oauth_token_refresh(tokens["refresh_token"], client_id)
        assert refreshed.status_code == 200, refreshed.text
        rotated = refreshed.json()
        assert rotated.get("access_token"), "refresh must mint a new access token"
        assert rotated.get("refresh_token") and rotated["refresh_token"] != tokens["refresh_token"], \
            "the refresh token must rotate"
        rotated_claims = decode_jwt_claims(rotated["access_token"])
        assert rotated_claims["sub"] == claims["sub"], "refresh must keep the same subject"
        assert rotated_claims["email"] == email
    finally:
        super_admin_user.delete_oauth_client(client_id)
