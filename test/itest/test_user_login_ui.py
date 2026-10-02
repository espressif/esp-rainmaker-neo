# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""The whole login story in one real browser: sign in, single sign-on to a second app, see the
device list, sign in a second device, remotely end it, and sign out for real.

Everything else in the suite scripts the flow with `requests`, which ignores SameSite, hardcodes the
provider's form field names, and -- decisively -- cannot prove that a real browser ACCEPTS our
__Host- prefixed cookies, or that the provider's own end-session hop happens on sign-out. A browser
enforces the cookie-prefix rules against the wire format and follows the full redirect chain; an HTTP
client does neither. So this is the one end-to-end test where the actor is a live Chromium, and it
walks the entire journey rather than asserting a single rule, because its failure means the product
does not work in a browser, whatever the server-side tests say.

Spec: federation.md, sso-sessions.md.

Requires playwright (declared in requirements.txt); the module skips cleanly without it.
"""
from urllib.parse import parse_qs, urlencode, urlparse

import pytest

from test.itest.conftest import (
    FLOW_COOKIE,
    SESSION_COOKIE,
    UPSTREAM_PROVIDER,
    USER_API_GATEWAY_URL,
    decode_jwt_claims,
    pkce_pair,
    requires_espuser,
)
from py_sdk.espuser_oauth import WebClient, client_ids_under, sids_in

pytest.importorskip("playwright.sync_api")

pytestmark = [pytest.mark.espuser, requires_espuser]

REDIRECT_A = "https://example.com/itest-login-ui/app-a/callback"
REDIRECT_B = "https://example.com/itest-login-ui/app-b/callback"
SIGNED_OUT_A = "https://example.com/itest-login-ui/app-a/signed-out"


def _authorize_url(client_id, redirect_uri, challenge, state,
                   scope="openid email account:sessions"):
    return f"{USER_API_GATEWAY_URL}/oauth2/authorize?" + urlencode({
        "response_type": "code", "client_id": client_id, "redirect_uri": redirect_uri,
        "scope": scope, "state": state,
        "code_challenge": challenge, "code_challenge_method": "S256",
    })


def _sign_in_through_the_hosted_ui(page, authorize_url, email, password, redirect_uri):
    """Drive a real sign-in through the provider's hosted UI, returning the URL the browser lands on
    (bearing code and state). Navigate the federation leg directly rather than picking a provider:
    with more than one enabled, authorize serves our own login page and there is no chooser on it yet.
    The hosted page renders the form twice (desktop and mobile) with duplicate ids, so pick the
    visible one rather than the first match."""
    page.goto(authorize_url, wait_until="domcontentloaded")
    page.goto(f"{USER_API_GATEWAY_URL}/oauth2/federation/start?provider={UPSTREAM_PROVIDER}",
              wait_until="domcontentloaded")
    assert "amazoncognito.com" in page.url, f"expected the upstream hosted UI, on {page.url}"
    page.locator("#signInFormUsername").locator("visible=true").first.fill(email)
    page.locator("#signInFormPassword").locator("visible=true").first.fill(password)
    with page.expect_navigation(url=lambda u: u.startswith(redirect_uri), timeout=30000):
        page.locator("input[name=signInSubmitButton]").locator("visible=true").first.click()
    return page.url


@pytest.fixture
def login_ui_clients(register_espuser_client):
    """Two products the person will open in one browser: app A carries a registered sign-out landing
    page, app B is only ever reached by single sign-on."""
    a = register_espuser_client(client_name="itest login-ui A", redirect_uris=[REDIRECT_A],
                                post_logout_redirect_uris=[SIGNED_OUT_A])
    b = register_espuser_client(client_name="itest login-ui B", redirect_uris=[REDIRECT_B])
    return (WebClient(a.client_id, REDIRECT_A, post_logout_redirect_uri=SIGNED_OUT_A),
            WebClient(b.client_id, REDIRECT_B))


def test_login_sso_sessions_and_signout_end_to_end(login_ui_clients, espuser_person, chromium_browser):
    """One person, two browsers, the whole journey -- each act commented for what only a real browser
    can prove."""
    app_a, app_b = login_ui_clients
    email, password = espuser_person.email, espuser_person.password

    device1 = chromium_browser.new_context()
    device2 = chromium_browser.new_context()
    try:
        # ── Act 1: sign in to app A in a real browser ──────────────────────────────────────────
        # /oauth2/authorize (PKCE, state) -> Cognito hosted UI -> authenticate -> back with code+state.
        page1 = device1.new_page()
        verifier_a, challenge_a = pkce_pair()
        page1.goto(_authorize_url(app_a.client_id, REDIRECT_A, challenge_a, "state-a"),
                   wait_until="domcontentloaded")
        # The flow cookie must be a __Host- cookie the browser accepts and will still send on the
        # cross-site return from the provider; wrong attributes are dropped SILENTLY and the login
        # then fails at the callback for no stated reason.
        flow = [c for c in device1.cookies() if c["name"] == FLOW_COOKIE]
        assert flow, f"authorize must set {FLOW_COOKIE} before leaving for the upstream"
        assert flow[0]["secure"] and flow[0]["httpOnly"] and flow[0]["path"] == "/", flow[0]
        assert flow[0]["sameSite"] in ("Lax", "None"), \
            f"{FLOW_COOKIE} SameSite={flow[0]['sameSite']} would be dropped on the return from upstream"

        page1.goto(f"{USER_API_GATEWAY_URL}/oauth2/federation/start?provider={UPSTREAM_PROVIDER}",
                   wait_until="domcontentloaded")
        assert "amazoncognito.com" in page1.url, f"expected the upstream hosted UI, on {page1.url}"
        page1.locator("#signInFormUsername").locator("visible=true").first.fill(email)
        page1.locator("#signInFormPassword").locator("visible=true").first.fill(password)
        with page1.expect_navigation(url=lambda u: u.startswith(REDIRECT_A), timeout=30000):
            page1.locator("input[name=signInSubmitButton]").locator("visible=true").first.click()

        landed = parse_qs(urlparse(page1.url).query)
        assert landed.get("code") and landed.get("state") == ["state-a"], page1.url
        # The session cookie SSO runs on, asserted from what the browser actually kept: __Host- means
        # Secure + Path=/ + Domain-less, HttpOnly keeps it out of script, and SameSite=Lax is what lets
        # it survive the top-level authorize navigation that single sign-on depends on.
        session = [c for c in device1.cookies() if c["name"] == SESSION_COOKIE]
        assert session, f"a completed login must leave {SESSION_COOKIE}; absent means the browser refused it"
        got = session[0]
        assert got["secure"] and got["httpOnly"] and got["path"] == "/", got
        assert got["sameSite"] == "Lax", f"{SESSION_COOKIE} SameSite must be Lax for SSO: {got}"

        exchanged = app_a.exchange_code(landed["code"][0], verifier_a)
        assert exchanged.status_code == 200, exchanged.text
        tokens_a = exchanged.json()
        assert tokens_a.get("access_token") and tokens_a.get("refresh_token") and tokens_a.get("id_token")
        claims_a = decode_jwt_claims(tokens_a["access_token"])
        assert "cognito-idp" not in claims_a["iss"], \
            f"the client must receive our token, never a Cognito-issued one: iss={claims_a['iss']}"
        assert claims_a["aud"] == app_a.client_id, f"token must be audienced to app A: {claims_a}"

        # ── Act 2: silent single sign-on to app B, same browser ────────────────────────────────
        # /oauth2/authorize returns a code immediately, with no login page and no trip to the provider.
        verifier_b, challenge_b = pkce_pair()
        page1.goto(_authorize_url(app_b.client_id, REDIRECT_B, challenge_b, "state-b"),
                   wait_until="domcontentloaded")
        assert page1.url.startswith(REDIRECT_B), \
            f"single sign-on must answer app B with a code, not a login page: {page1.url}"
        assert "amazoncognito.com" not in page1.url, \
            f"app B went back to the provider; that is not single sign-on: {page1.url}"
        landed_b = parse_qs(urlparse(page1.url).query)
        assert landed_b.get("code") and landed_b.get("state") == ["state-b"], page1.url
        exchanged_b = app_b.exchange_code(landed_b["code"][0], verifier_b)
        assert exchanged_b.status_code == 200, exchanged_b.text
        claims_b = decode_jwt_claims(exchanged_b.json()["access_token"])
        assert claims_b["sub"] == claims_a["sub"], "single sign-on must resolve to the same person"
        assert claims_b["aud"] == app_b.client_id, f"token must be audienced to app B: {claims_b}"

        # ── Act 3: the device list -- one browser, both apps nested under it ────────────────────
        listing = WebClient.sessions(tokens_a["access_token"])
        assert listing.status_code == 200, listing.text
        body = listing.json()
        assert len(body["sessions"]) == 1, f"one browser is one device: {body}"
        view = body["sessions"][0]
        assert view["current"] is True, view
        assert client_ids_under(view) == sorted([app_a.client_id, app_b.client_id]), view

        # ── Act 4: a second browser (device 2) signs the same person in ─────────────────────────
        page2 = device2.new_page()
        verifier_a2, challenge_a2 = pkce_pair()
        landed2 = _sign_in_through_the_hosted_ui(
            page2, _authorize_url(app_a.client_id, REDIRECT_A, challenge_a2, "state-a2"),
            email, password, REDIRECT_A)
        code2 = parse_qs(urlparse(landed2).query).get("code")
        assert code2, landed2
        exchanged2 = app_a.exchange_code(code2[0], verifier_a2)
        assert exchanged2.status_code == 200, exchanged2.text
        tokens_a2 = exchanged2.json()

        two = WebClient.sessions(tokens_a["access_token"]).json()
        assert len(two["sessions"]) == 2, f"two browsers, two rows: {two}"

        # ── Act 5: end device 2 remotely from device 1 ─────────────────────────────────────────
        device2_sid = [s["session_id"] for s in two["sessions"] if not s["current"]][0]
        ended = WebClient.end_session(tokens_a["access_token"], device2_sid)
        assert ended.status_code == 204, f"{ended.status_code} {ended.text[:200]}"
        assert app_a.refresh(tokens_a2["refresh_token"]).status_code != 200, \
            "device 2 must no longer be able to refresh once it has been ended"
        after = WebClient.sessions(tokens_a["access_token"]).json()
        assert device2_sid not in sids_in(after), after
        assert len(after["sessions"]) == 1 and after["sessions"][0]["current"], \
            f"device 1 must be untouched and still list: {after}"

        # ── Act 6: real RP-initiated logout on device 1, through the provider's end-session hop ──
        # Device 1 is fully alive right up to the moment it signs out.
        alive = app_a.refresh(tokens_a["refresh_token"])
        assert alive.status_code == 200, f"device 1 must still refresh before it signs out: {alive.text[:200]}"
        device1_refresh = alive.json()["refresh_token"]

        seen = []
        page1.on("request", lambda r: seen.append(r.url))
        page1.goto(f"{USER_API_GATEWAY_URL}/oauth2/logout?" + urlencode({
            "client_id": app_a.client_id,
            "post_logout_redirect_uri": SIGNED_OUT_A,
            "id_token_hint": tokens_a.get("id_token", ""),
        }), wait_until="domcontentloaded")

        assert page1.url.startswith(SIGNED_OUT_A), \
            f"sign-out must land on the registered return URL: {page1.url}"
        # The provider's own logout must actually happen -- the browser has to pass through Cognito's
        # end-session endpoint, not just clear our cookie and go home.
        assert any("amazoncognito.com" in u and "logout" in u for u in seen), \
            f"the browser never passed through the provider's end-session endpoint: {seen}"
        assert not [c for c in device1.cookies() if c["name"] == SESSION_COOKIE], \
            "the sign-out must clear the session cookie in the browser"
        assert app_a.refresh(device1_refresh).status_code != 200, \
            "the refresh family must be dead after sign-out"

        # And a fresh authorize now requires logging in again -- no silent code from a dead session.
        _, challenge_after = pkce_pair()
        page1.goto(_authorize_url(app_a.client_id, REDIRECT_A, challenge_after, "state-after"),
                   wait_until="domcontentloaded")
        assert not page1.url.startswith(REDIRECT_A), \
            f"a signed-out browser must be asked to log in again, not handed a code: {page1.url}"
    finally:
        device1.close()
        device2.close()
