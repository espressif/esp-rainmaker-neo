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
import json
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, HTTPServer
from types import SimpleNamespace
from urllib.parse import parse_qs, urlencode, urlparse

import boto3
import pytest
import requests
from botocore.exceptions import ClientError

from py_sdk.espuser_oauth import Browser, WebClient, client_ids_under, sids_in
from test.itest.conftest import (
    END_USER_POOL_ID,
    ESPUSER_RESOURCE,
    FLOW_COOKIE,
    INBUILT_PROVIDER,
    PROVIDER_SETTLE,
    REGION,
    SESSION_COOKIE,
    USER_API_GATEWAY_URL,
    decode_jwt_claims,
    esp_user_base_outputs,
    pkce_pair,
    providers_table,
    remove_end_users,
    requires_espuser,
)
from test.itest.email_utils import (
    delete_mailosaur_messages_for_email,
    generate_mailosaur_email_specific,
    generate_random_email,
    generate_test_password,
    get_verification_code_from_server,
)

pytest.importorskip("playwright.sync_api")

# Not env_mut: sync Playwright refuses to share a worker with test_oauth_proxy.py's asyncio loop, and the rows written here are itest- rows that sort after the built-in one, so no other login changes meaning; only the opt-in cognito matrix rewrites the shared rows, and it is deselected by default.
pytestmark = [pytest.mark.espuser, requires_espuser, pytest.mark.xdist_group("browser")]

REDIRECT_A = "https://example.com/itest-login-ui/app-a/callback"
REDIRECT_B = "https://example.com/itest-login-ui/app-b/callback"
SIGNED_OUT_A = "https://example.com/itest-login-ui/app-a/signed-out"


def _authorize_url(client_id, redirect_uri, challenge, state,
                   scope="openid email account:sessions", **hints):
    return f"{USER_API_GATEWAY_URL}/oauth2/authorize?" + urlencode({
        "response_type": "code", "client_id": client_id, "redirect_uri": redirect_uri,
        "scope": scope, "state": state,
        "code_challenge": challenge, "code_challenge_method": "S256", **hints,
    })


def _sign_in_through_the_hosted_ui(page, authorize_url, email, password, redirect_uri, provider):
    """Drive a real sign-in through the provider's hosted UI, returning the URL the browser lands on
    (bearing code and state). Navigate the federation leg directly rather than clicking through our
    own sign-in page: this test is about the upstream leg and the cookies it depends on, and our page
    has tests of its own below. The hosted page renders the form twice (desktop and mobile) with duplicate ids, so pick the
    visible one rather than the first match."""
    page.goto(authorize_url, wait_until="domcontentloaded")
    flow_id = next(c["value"] for c in page.context.cookies() if c["name"] == FLOW_COOKIE)
    page.goto(f"{USER_API_GATEWAY_URL}/user/login/federation/start?provider={provider}&flow_id={flow_id}",
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


def test_login_sso_sessions_and_signout_end_to_end(login_ui_clients, espuser_person, chromium_browser,
                                                   hosted_cognito_provider):
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

        page1.goto(f"{USER_API_GATEWAY_URL}/user/login/federation/start?provider={hosted_cognito_provider}&flow_id={flow[0]['value']}",
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
        assert claims_a["aud"] == ESPUSER_RESOURCE, f"token must be for app A's default resource, never its id: {claims_a}"

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
        assert claims_b["aud"] == ESPUSER_RESOURCE, f"token must be for app B's default resource, never its id: {claims_b}"

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
            email, password, REDIRECT_A, hosted_cognito_provider)
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


# The sign-in page is a compiled program served through Go and the gateway, and only a browser can tell that chain works. Spec: federation.md ("The sign-in app").

SIGN_IN_REDIRECT = "https://example.com/itest-signin-page/callback"
SECOND_PROVIDER = "itest-signin-page-second"

# Registry keys that are also the brand a person knows, so these appearing on screen is correct.
# Any other key is an internal word and must not reach the page.
BRAND_IS_THE_NAME = {"google", "github"}


@pytest.fixture
def sign_in_page_client(register_espuser_client):
    """The product whose login these tests drive. Named, because the page says what you are signing
    in to, and the page reads that name from the flow's client."""
    made = register_espuser_client(client_name="itest sign-in page",
                                   redirect_uris=[SIGN_IN_REDIRECT])
    return WebClient(made.client_id, SIGN_IN_REDIRECT)


@pytest.fixture
def a_second_provider():
    """An oidc row whose registry key is an internal word, so the page can be checked for showing its display name instead.

    It takes no password and sorts after the built-in row, so no other login changes meaning. Removed in teardown: left behind, it would put a button that goes nowhere on every later login.
    """
    providers_table().put_item(Item={
        "provider_name": SECOND_PROVIDER,
        "type": "oidc",
        "display_name": "Example SSO",
        "enabled": True,
        "issuer": "https://example.com/itest-second-provider",
        "client_id": "itest-second-provider",
        "scopes": "openid email",
    })
    time.sleep(PROVIDER_SETTLE)
    try:
        yield SECOND_PROVIDER
    finally:
        providers_table().delete_item(Key={"provider_name": SECOND_PROVIDER})


def test_the_sign_in_page_draws_itself_from_the_description_it_was_served_with(
        sign_in_page_client, a_second_provider, chromium_browser):
    """One navigation must produce a usable form -- no second request, no spinner.

    The page carries its own description, so this asserts the whole serving chain at once: the
    embedded bundle loaded through the gateway, Go's substitution produced parsable JSON, and the
    app booted from it. A broken link anywhere leaves a blank card, which every server-side test
    would still call a 200.
    """
    context = chromium_browser.new_context()
    try:
        page = context.new_page()
        broken = []
        page.on("requestfailed", lambda r: broken.append(r.url))
        _, challenge = pkce_pair()
        page.goto(_authorize_url(sign_in_page_client.client_id, SIGN_IN_REDIRECT,
                                 challenge, "state-page"), wait_until="load")

        described = page.locator("#esp-page-data").text_content()
        assert described, "the page must arrive describing itself; without it the app has nothing to draw"
        data = json.loads(described)
        assert data["providers"], f"the registry reached the page empty: {data}"

        # Drawn, not merely delivered: a bundle that 404s leaves the JSON above intact.
        page.wait_for_selector("#username", timeout=10000)
        assert not broken, f"the page could not load part of itself: {broken}"
        heading = page.get_by_role("heading", level=1).inner_text()
        assert heading == "Sign in", f"the heading must stay short whatever the client is called: {heading!r}"
        client = page.locator(".client").text_content()
        assert client == "Signing in to itest sign-in page", f"the page must name the client being signed in to: {client!r}"

        shown = page.inner_text("body")
        for provider in data["providers"]:
            if provider["name"].lower() in BRAND_IS_THE_NAME:
                continue
            assert provider["name"].lower() not in shown.lower(), (
                f"the registry key {provider['name']!r} reached the screen; it is an internal word "
                "and the page must show the operator's display name instead")

        # A form a password manager will not fill reads to a person as a broken site.
        assert page.get_attribute("#username", "autocomplete") == "username"
        assert page.get_attribute("#password", "autocomplete") == "current-password"
        assert page.get_attribute("#password", "type") == "password", \
            "the password must not be on screen in clear before anyone asks for it"
    finally:
        context.close()


def test_a_password_sign_in_ends_at_the_client_with_a_code_and_no_token_in_the_browser(
        sign_in_page_client, espuser_person, chromium_browser):
    """The page drives the flow and holds nothing.

    A token answered here would land in a browser that never proved possession of the PKCE verifier,
    so the code binding the client relies on would be bypassed by the page meant to serve it.
    """
    context = chromium_browser.new_context()
    try:
        page = context.new_page()
        verifier, challenge = pkce_pair()
        page.goto(_authorize_url(sign_in_page_client.client_id, SIGN_IN_REDIRECT,
                                 challenge, "state-page"), wait_until="load")
        page.wait_for_selector("#username", timeout=10000)
        page.fill("#username", espuser_person.email)
        page.fill("#password", espuser_person.password)

        # Captured through the route rather than read off the Response afterwards. A successful
        # sign-in navigates immediately, and Chromium discards the body of a response belonging to a
        # document that is gone -- "No resource with given identifier found". Reading it inside the
        # interception happens before the page can leave.
        answered = {}

        def capture(route):
            reply = route.fetch()
            answered["status"] = reply.status
            answered["body"] = reply.json()
            route.fulfill(response=reply)

        page.route("**/user/login/password", capture)
        with page.expect_navigation(url=lambda u: u.startswith(SIGN_IN_REDIRECT), timeout=30000):
            page.click("button[type=submit]")
        page.unroute("**/user/login/password")
        assert answered, "the page never called the password endpoint"
        body = answered["body"]

        assert "redirect_to" in body, f"a browser mid-flow must be given somewhere to go: {body}"
        assert "access_token" not in body and "id_token" not in body and "refresh_token" not in body, \
            f"the sign-in page was handed a spendable credential: {sorted(body)}"

        landed = parse_qs(urlparse(page.url).query)
        assert landed.get("state") == ["state-page"], page.url
        code = landed.get("code")
        assert code, f"the flow must end at the client with a code: {page.url}"

        # And the code is real: only redeeming it proves the page completed the same flow it started.
        exchanged = sign_in_page_client.exchange_code(code[0], verifier)
        assert exchanged.status_code == 200, exchanged.text
        assert exchanged.json().get("access_token")
    finally:
        context.close()


def test_an_expired_sign_in_starts_again_from_the_page_and_finishes(
        sign_in_page_client, espuser_person, chromium_browser):
    """A sign-in left open past its lifetime ends on Start again, which replays the client's own request and still finishes with a code."""
    context = chromium_browser.new_context()
    try:
        page = context.new_page()
        verifier, challenge = pkce_pair()
        page.goto(_authorize_url(sign_in_page_client.client_id, SIGN_IN_REDIRECT,
                                 challenge, "state-restart"), wait_until="load")
        page.wait_for_selector("#username", timeout=10000)
        flow_id = next(c["value"] for c in context.cookies() if c["name"] == FLOW_COOKIE)
        boto3.resource("dynamodb", region_name=REGION).Table("espuser-auth-flows").update_item(
            Key={"flow_id": flow_id}, UpdateExpression="SET expires_on = :past",
            ExpressionAttributeValues={":past": int(time.time()) - 60})

        page.fill("#username", espuser_person.email)
        page.fill("#password", espuser_person.password)
        page.click("button[type=submit]")
        page.get_by_role("button", name="Start again").click(timeout=15000)

        page.wait_for_selector("#username", timeout=15000)
        restarted = next(c["value"] for c in context.cookies() if c["name"] == FLOW_COOKIE)
        assert restarted != flow_id, "Start again must open a new sign-in, not revive the expired one"
        page.fill("#username", espuser_person.email)
        _submit_password(page, espuser_person.password, SIGN_IN_REDIRECT)
        _redeem(sign_in_page_client, page, verifier, "state-restart")
    finally:
        context.close()


def test_a_wrong_password_is_answered_the_same_as_an_unknown_account(
        sign_in_page_client, espuser_person, chromium_browser):
    """The negative half of the same form.

    Two things at once because they are one property: the person is told enough to try again, and
    nobody watching is told whether the address is registered. A page that named the reason would
    answer the second question every time it answered the first.
    """
    context = chromium_browser.new_context()
    try:
        page = context.new_page()
        _, challenge = pkce_pair()
        page.goto(_authorize_url(sign_in_page_client.client_id, SIGN_IN_REDIRECT,
                                 challenge, "state-page"), wait_until="load")
        page.wait_for_selector("#username", timeout=10000)

        def refused(username, password):
            """One attempt, waited on by its own response rather than by the screen.

            The assertion is that both attempts look identical, so there is no DOM change to wait
            for -- polling the alert would read the previous attempt's text and pass for the wrong
            reason. The response is the one event that definitely belongs to this submission.
            """
            page.fill("#username", username)
            page.fill("#password", password)
            with page.expect_response(lambda r: "/user/login/password" in r.url) as answered:
                page.click("button[type=submit]")
            reply = answered.value
            return reply.status, reply.json()

        real_status, real_body = refused(espuser_person.email, "not-" + espuser_person.password)
        no_status, no_body = refused(f"nobody-{uuid.uuid4().hex[:12]}@example.com", "not-a-password")

        assert (real_status, real_body) == (no_status, no_body), (
            "a registered address and an unknown one are answered differently, which makes the form "
            f"a way to test whether someone has an account: {real_body} vs {no_body}")
        assert real_status == 401, real_body
        # The operator's detail is written for a log, and names pools and API calls.
        assert "pool" not in json.dumps(real_body).lower(), real_body

        # And what the person actually reads is the generic copy, with the login still open to them:
        # an error must leave a way forward, not end the attempt.
        page.wait_for_selector("[role=alert]", timeout=10000)
        shown = page.inner_text("[role=alert]")
        assert shown.strip(), "a refusal with nothing on screen is the dead end this page removes"
        assert "pool" not in shown.lower() and "InitiateAuth" not in shown, shown
        assert page.is_visible("#password"), "a failed attempt left no way to try again"
        assert not page.url.startswith(SIGN_IN_REDIRECT), \
            f"a rejected password must not complete the flow: {page.url}"
    finally:
        context.close()


CONSENT_REDIRECT = "https://example.com/itest-login-ui/third-party/callback"


def test_a_third_party_app_gets_a_code_only_after_the_person_allows_it(
        register_espuser_client, espuser_person, chromium_browser):
    """RFC 6749 s10.2 / RFC 9700 s4.11.2: a client we do not ship never receives a code on a sign-in alone.

    Both doors are walked in one browser: a fresh sign-in, then a second request answered by the
    session, which without consent would hand the app a code with no click at all.
    """
    made = register_espuser_client(client_name="itest third-party app", first_party=False,
                                   redirect_uris=[CONSENT_REDIRECT])
    app = WebClient(made.client_id, CONSENT_REDIRECT)
    context = chromium_browser.new_context()
    try:
        page = context.new_page()
        verifier, challenge = pkce_pair()
        page.goto(_authorize_url(app.client_id, CONSENT_REDIRECT, challenge, "state-allow"), wait_until="load")
        page.wait_for_selector("#username", timeout=10000)
        page.fill("#username", espuser_person.email)
        page.fill("#password", espuser_person.password)
        with page.expect_navigation(url=lambda u: "/user/login/consent" in u, timeout=30000):
            page.click("button[type=submit]")

        page.wait_for_selector(".grants", timeout=10000)
        assert "itest third-party app" in page.inner_text(".consent-lede")
        assert espuser_person.email in page.inner_text(".consent-account")

        with page.expect_navigation(url=lambda u: u.startswith(CONSENT_REDIRECT), timeout=30000):
            page.get_by_role("button", name="Allow").click()
        landed = parse_qs(urlparse(page.url).query)
        assert landed.get("state") == ["state-allow"], page.url
        exchanged = app.exchange_code(landed["code"][0], verifier)
        assert exchanged.status_code == 200, exchanged.text

        # A live session answers this request, which is exactly the zero-click case consent guards.
        _, challenge = pkce_pair()
        page.goto(_authorize_url(app.client_id, CONSENT_REDIRECT, challenge, "state-deny"), wait_until="load")
        assert "/user/login/consent" in page.url, f"a live session must still ask: {page.url}"
        page.wait_for_selector(".grants", timeout=10000)
        with page.expect_navigation(url=lambda u: u.startswith(CONSENT_REDIRECT), timeout=30000):
            page.get_by_role("button", name="Cancel").click()
        landed = parse_qs(urlparse(page.url).query)
        assert landed.get("error") == ["access_denied"], page.url
        assert landed.get("state") == ["state-deny"], page.url
        assert "code" not in landed, page.url
    finally:
        context.close()


SIGN_OUT_APP_REDIRECT = "https://example.com/itest-login-ui/sign-out-app/callback"
SIGN_OUT_APP_SIGNED_OUT = "https://example.com/itest-login-ui/sign-out-app/signed-out"


def test_a_sign_out_without_a_hint_is_asked_and_only_the_persons_yes_ends_it(
        register_espuser_client, espuser_person, chromium_browser):
    """Any page can send a browser to /oauth2/logout, so without an id_token_hint from this session the
    person is asked (RP-Initiated Logout 1.0 s2, s6). Cancel keeps the session and says so on our page,
    never going on to the app's post-logout page, which would read as a sign-out that never happened;
    Sign out ends it and returns the person to the app that asked, with its state."""
    made = register_espuser_client(client_name="itest sign-out app", redirect_uris=[SIGN_OUT_APP_REDIRECT],
                                   post_logout_redirect_uris=[SIGN_OUT_APP_SIGNED_OUT])
    app = WebClient(made.client_id, SIGN_OUT_APP_REDIRECT, post_logout_redirect_uri=SIGN_OUT_APP_SIGNED_OUT)
    context = chromium_browser.new_context()
    try:
        page = context.new_page()
        verifier, challenge = pkce_pair()
        page.goto(_authorize_url(app.client_id, SIGN_OUT_APP_REDIRECT, challenge, "state-in"), wait_until="load")
        page.wait_for_selector("#username", timeout=10000)
        page.fill("#username", espuser_person.email)
        page.fill("#password", espuser_person.password)
        with page.expect_navigation(url=lambda u: u.startswith(SIGN_OUT_APP_REDIRECT), timeout=30000):
            page.click("button[type=submit]")
        exchanged = app.exchange_code(parse_qs(urlparse(page.url).query)["code"][0], verifier)
        assert exchanged.status_code == 200, exchanged.text
        refresh_token = exchanged.json()["refresh_token"]
        sign_out = f"{USER_API_GATEWAY_URL}/oauth2/logout?" + urlencode({
            "client_id": app.client_id, "post_logout_redirect_uri": SIGN_OUT_APP_SIGNED_OUT, "state": "state-out",
        })

        page.goto(sign_out, wait_until="load")
        assert "/user/logout" in page.url, f"a sign-out without a hint must be asked: {page.url}"
        page.wait_for_selector(".consent-actions", timeout=10000)
        assert espuser_person.email in page.inner_text(".consent-account")
        page.get_by_role("button", name="Cancel").click()
        page.get_by_role("heading", name="You’re still signed in").wait_for(timeout=10000)
        assert "/user/logout" in page.url, f"Cancel must stay on our page: {page.url}"
        assert [c for c in context.cookies() if c["name"] == SESSION_COOKIE], "Cancel must keep the session cookie"
        assert app.refresh(refresh_token).status_code == 200, "Cancel must end nothing"

        page.goto(sign_out, wait_until="load")
        page.wait_for_selector(".consent-actions", timeout=10000)
        with page.expect_navigation(url=lambda u: u.startswith(SIGN_OUT_APP_SIGNED_OUT), timeout=30000):
            page.get_by_role("button", name="Sign out").click()
        assert parse_qs(urlparse(page.url).query).get("state") == ["state-out"], page.url
        assert not [c for c in context.cookies() if c["name"] == SESSION_COOKIE], \
            "the confirmed sign-out must clear the session cookie in the browser"
        assert app.refresh(refresh_token).status_code != 200, "the refresh family must be dead after the sign-out"
    finally:
        context.close()



REVOKE_WEB_REDIRECT = "https://example.com/itest-login-ui/revoke-web/callback"


class _AnswerOk(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()

    def log_message(self, *args):
        pass


@pytest.fixture
def loopback_redirect():
    """An http loopback redirect URI with a listener behind it: a native app may not register https, and Playwright routing never sees a redirect hop, so Chromium needs a real answer there."""
    server = HTTPServer(("127.0.0.1", 0), _AnswerOk)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    yield f"http://127.0.0.1:{server.server_port}/itest-login-ui/revoke-app/callback"
    server.shutdown()
    server.server_close()


def test_an_app_revoking_its_login_leaves_the_browser_signed_in(register_espuser_client, espuser_person, chromium_browser, loopback_redirect):
    """A native app signed in through the browser signs out by revoking its own refresh token (RFC 7009); in that same real browser the session cookie stays, and the web app signs in again straight from it, with no sign-in page."""
    web_made = register_espuser_client(client_name="itest revoke web app", redirect_uris=[REVOKE_WEB_REDIRECT])
    app_made = register_espuser_client(client_name="itest revoke native app", redirect_uris=[loopback_redirect],
                                       application_type="native")
    web = WebClient(web_made.client_id, REVOKE_WEB_REDIRECT)
    app = WebClient(app_made.client_id, loopback_redirect)
    context = chromium_browser.new_context()
    try:
        page = context.new_page()
        verifier, challenge = pkce_pair()
        page.goto(_authorize_url(web.client_id, REVOKE_WEB_REDIRECT, challenge, "state-web"), wait_until="load")
        page.wait_for_selector("#username", timeout=10000)
        page.fill("#username", espuser_person.email)
        page.fill("#password", espuser_person.password)
        with page.expect_navigation(url=lambda u: u.startswith(REVOKE_WEB_REDIRECT), timeout=30000):
            page.click("button[type=submit]")
        web_login = web.exchange_code(parse_qs(urlparse(page.url).query)["code"][0], verifier)
        assert web_login.status_code == 200, web_login.text

        # The browser is signed in, so the native app's authorize answers straight from the session cookie.
        verifier, challenge = pkce_pair()
        page.goto(_authorize_url(app.client_id, loopback_redirect, challenge, "state-app"), wait_until="load")
        assert page.url.startswith(loopback_redirect), f"the signed-in browser must return the app a code: {page.url}"
        app_login = app.exchange_code(parse_qs(urlparse(page.url).query)["code"][0], verifier)
        assert app_login.status_code == 200, app_login.text
        app_refresh = app_login.json()["refresh_token"]

        revoked = app.revoke(app_refresh)
        assert revoked.status_code == 200, revoked.text
        assert app.refresh(app_refresh).status_code != 200, "the app that signed out is signed out"

        assert [c for c in context.cookies() if c["name"] == SESSION_COOKIE], "the browser keeps its session cookie"
        assert web.refresh(web_login.json()["refresh_token"]).status_code == 200, "the web app's login still works"
        verifier, challenge = pkce_pair()
        page.goto(_authorize_url(web.client_id, REVOKE_WEB_REDIRECT, challenge, "state-again"), wait_until="load")
        assert page.url.startswith(REVOKE_WEB_REDIRECT), f"the browser must sign in again without the sign-in page: {page.url}"
        assert web.exchange_code(parse_qs(urlparse(page.url).query)["code"][0], verifier).status_code == 200
    finally:
        context.close()

# The account and second-pool journeys on our page. Spec: federation.md ("The sign-in app"), external-provider.md.

ISSUER = (esp_user_base_outputs.get("EspUserDiscoveryIssuer") or "").rstrip("/")

LIFECYCLE_REDIRECT = "https://example.com/itest-login-journeys/lifecycle/callback"
SECOND_POOL_REDIRECT = "https://example.com/itest-login-journeys/second-pool/callback"

# Long enough for Cognito to mail a code and Mailosaur to receive it.
CODE_WAIT = {"max_retries": 6, "retry_delay": 3.0, "timeout": 45.0}

# The page's copy for invalid_credentials (hosted-ui/src/api/errors.ts).
WRONG_PASSWORD_COPY = "That email or phone number and password don’t match. Try again, or reset your password."

POOL_LABEL = "itest second pool"
HOSTED_LABEL = "itest second pool hosted page"

# A new pool's hosted-UI domain takes a minute or more to resolve and serve.
DOMAIN_SERVE_DEADLINE = 300.0
# A lookup before the name is published gets NXDOMAIN, which the local resolver then caches for longer than the deadline.
DOMAIN_DNS_GRACE = 60.0


def _submit_password(page, password, lands_at):
    """Submit the page's password form, follow it to the client, and return the provider the page named.

    The reply is read inside the route because Chromium drops a response body once its document has navigated away.
    """
    seen = {}

    def capture(route):
        reply = route.fetch()
        seen["body"] = reply.json()
        seen["provider"] = json.loads(route.request.post_data or "{}").get("provider")
        route.fulfill(response=reply)

    page.route("**/user/login/password", capture)
    page.fill("#password", password)
    with page.expect_navigation(url=lambda u: u.startswith(lands_at), timeout=30000):
        page.click("button[type=submit]")
    page.unroute("**/user/login/password")
    assert seen, "the page never called the password endpoint"
    assert not {"access_token", "id_token", "refresh_token"} & set(seen["body"]), \
        f"the sign-in page was handed a spendable credential: {sorted(seen['body'])}"
    return seen["provider"]


def _redeem(app, page, verifier, state):
    """The code the browser landed with, exchanged as the client would; returns the access token's claims."""
    landed = parse_qs(urlparse(page.url).query)
    assert landed.get("state") == [state], page.url
    assert landed.get("code"), f"the flow must end at the client with a code: {page.url}"
    exchanged = app.exchange_code(landed["code"][0], verifier)
    assert exchanged.status_code == 200, exchanged.text
    return decode_jwt_claims(exchanged.json()["access_token"])


def _mailed_code(email, since):
    code = get_verification_code_from_server(recipient_email=email, since_timestamp=since, **CODE_WAIT)
    assert code, f"no code was mailed to {email}"
    return code


@pytest.fixture
def lifecycle_client(register_espuser_client):
    made = register_espuser_client(client_name="itest account lifecycle", redirect_uris=[LIFECYCLE_REDIRECT])
    return WebClient(made.client_id, LIFECYCLE_REDIRECT)


# Opt-in: Cognito really mails both codes, which the sandbox and the account's email quota limit.
def test_an_account_made_on_our_page_signs_in_and_resets_its_password(
        lifecycle_client, chromium_browser, require_mailosaur):
    """Sign up, verify with the mailed code, sign in; then reset the password with the mailed code, after which only the new one signs in."""
    email = generate_mailosaur_email_specific(f"itest-signup-{uuid.uuid4().hex[:12]}")
    if not email:
        pytest.skip("Mailosaur address unavailable")
    password, new_password = generate_test_password(), generate_test_password()
    # Named so a concurrent test's extra pool never decides which pool the account is made in.
    hint = {"idp": INBUILT_PROVIDER}
    first, second = chromium_browser.new_context(), chromium_browser.new_context()
    try:
        page = first.new_page()
        verifier, challenge = pkce_pair()
        page.goto(_authorize_url(lifecycle_client.client_id, LIFECYCLE_REDIRECT, challenge, "state-signup", **hint),
                  wait_until="load")
        page.get_by_role("button", name="Create an account", exact=True).click()
        page.fill("#acct-username", email)
        page.fill("#acct-password", password)
        mailed_after = time.time()
        page.get_by_role("button", name="Create account", exact=True).click()
        page.wait_for_selector("#acct-code", timeout=15000)
        page.fill("#acct-code", _mailed_code(email, mailed_after))
        page.get_by_role("button", name="Continue", exact=True).click()
        page.get_by_text("Your account is verified").wait_for(timeout=15000)
        page.get_by_role("button", name="Sign in", exact=True).click()
        page.wait_for_selector("#password", timeout=10000)
        assert page.input_value("#username") == email, "the address typed at sign-up must carry over to sign-in"
        _submit_password(page, password, LIFECYCLE_REDIRECT)
        _redeem(lifecycle_client, page, verifier, "state-signup")

        # A second browser, because the first one's session would answer authorize without the page.
        page = second.new_page()
        verifier, challenge = pkce_pair()
        page.goto(_authorize_url(lifecycle_client.client_id, LIFECYCLE_REDIRECT, challenge, "state-reset", **hint),
                  wait_until="load")
        page.get_by_role("button", name="Forgot password?", exact=True).click()
        page.fill("#acct-username", email)
        mailed_after = time.time()
        page.get_by_role("button", name="Continue", exact=True).click()
        page.wait_for_selector("#acct-code", timeout=15000)
        page.fill("#acct-code", _mailed_code(email, mailed_after))
        page.fill("#acct-password", new_password)
        page.get_by_role("button", name="Update password", exact=True).click()
        page.get_by_text("Your password is set").wait_for(timeout=15000)
        page.get_by_role("button", name="Sign in", exact=True).click()
        page.wait_for_selector("#password", timeout=10000)

        page.fill("#password", password)
        with page.expect_response(lambda r: "/user/login/password" in r.url) as answered:
            page.click("button[type=submit]")
        assert answered.value.status == 401, answered.value.text()
        assert answered.value.json().get("error") == "invalid_credentials", answered.value.text()
        page.locator("#signin-error").get_by_text(WRONG_PASSWORD_COPY).wait_for(timeout=10000)

        _submit_password(page, new_password, LIFECYCLE_REDIRECT)
        _redeem(lifecycle_client, page, verifier, "state-reset")
    finally:
        first.close()
        second.close()
        remove_end_users(END_USER_POOL_ID, [(email, email, None)])
        delete_mailosaur_messages_for_email(email)


def _attempt(step, **kwargs):
    """Run one teardown step, reporting rather than raising, so a failed step never skips the ones after it."""
    try:
        step(**kwargs)
    except Exception as e:  # noqa: BLE001
        print(f"Warning: teardown step failed: {e}")


def _wait_until_serving(hosted_ui, client_id, callback):
    deadline = time.time() + DOMAIN_SERVE_DEADLINE
    time.sleep(DOMAIN_DNS_GRACE)
    while True:
        try:
            if requests.get(f"{hosted_ui}/login", params={
                    "client_id": client_id, "response_type": "code", "redirect_uri": callback}, timeout=10).ok:
                return
        except requests.RequestException:
            pass
        assert time.time() < deadline, f"{hosted_ui} did not start serving within {DOMAIN_SERVE_DEADLINE:.0f}s"
        time.sleep(5)


@pytest.fixture(scope="module")
def second_pool():
    """A Cognito pool made for this module, registered twice: an oidc row naming password_verifier cognito, whose password our page takes, and an oidc row naming none, which sends the browser to the pool's hosted UI.

    Everything is named with an itest- prefix and removed afterwards, rows first, even when a step fails.
    """
    callback = f"{ISSUER}/oauth2/federation/callback"
    suffix = uuid.uuid4().hex[:10]
    cognito = boto3.client("cognito-idp", region_name=REGION)
    try:
        pool_id = cognito.create_user_pool(
            PoolName=f"itest-second-pool-{suffix}", UsernameAttributes=["email"],
            AutoVerifiedAttributes=["email"])["UserPool"]["Id"]
    except ClientError as e:
        pytest.skip(f"cannot create a test user pool: {e}")

    person = SimpleNamespace(email=generate_random_email(), password=generate_test_password())
    rows = {"cognito": f"itest-second-pool-{suffix}", "oidc": f"itest-second-pool-hosted-{suffix}"}
    client_id = domain = None
    try:
        client = cognito.create_user_pool_client(
            UserPoolId=pool_id, ClientName=f"itest-second-pool-{suffix}", GenerateSecret=True,
            ExplicitAuthFlows=["ALLOW_USER_PASSWORD_AUTH", "ALLOW_REFRESH_TOKEN_AUTH"],
            SupportedIdentityProviders=["COGNITO"], CallbackURLs=[callback],
            LogoutURLs=[f"{ISSUER}/oauth2/federation/logout/callback"], AllowedOAuthFlows=["code"],
            AllowedOAuthScopes=["openid", "email"], AllowedOAuthFlowsUserPoolClient=True,
            PreventUserExistenceErrors="ENABLED")["UserPoolClient"]
        client_id = client["ClientId"]
        # Version 1 is the classic hosted UI, whose form fields the browser below fills.
        cognito.create_user_pool_domain(Domain=f"itest-{suffix}", UserPoolId=pool_id, ManagedLoginVersion=1)
        domain = f"itest-{suffix}"
        cognito.admin_create_user(
            UserPoolId=pool_id, Username=person.email, MessageAction="SUPPRESS",
            UserAttributes=[{"Name": "email", "Value": person.email}, {"Name": "email_verified", "Value": "true"}])
        cognito.admin_set_user_password(
            UserPoolId=pool_id, Username=person.email, Password=person.password, Permanent=True)
        hosted_ui = f"https://{domain}.auth.{REGION}.amazoncognito.com"
        _wait_until_serving(hosted_ui, client_id, callback)

        connection = {
            "enabled": True,
            "issuer": f"https://cognito-idp.{REGION}.amazonaws.com/{pool_id}",
            "client_id": client_id,
            "client_secret": client["ClientSecret"],
            "token_endpoint_auth": "client_secret_basic",
            "scopes": "openid email",
        }
        providers_table().put_item(Item={**connection, "provider_name": rows["cognito"], "type": "oidc",
                                         "password_verifier": "cognito", "display_name": POOL_LABEL})
        providers_table().put_item(Item={**connection, "provider_name": rows["oidc"], "type": "oidc",
                                         "display_name": HOSTED_LABEL})
        time.sleep(PROVIDER_SETTLE)
        yield SimpleNamespace(person=person, cognito_row=rows["cognito"], oidc_row=rows["oidc"],
                              hosted_host=urlparse(hosted_ui).netloc)
    finally:
        for name in rows.values():
            _attempt(providers_table().delete_item, Key={"provider_name": name})
        remove_end_users(pool_id, [(person.email, person.email, None)])
        if domain:
            _attempt(cognito.delete_user_pool_domain, Domain=domain, UserPoolId=pool_id)
        if client_id:
            _attempt(cognito.delete_user_pool_client, UserPoolId=pool_id, ClientId=client_id)
        _attempt(cognito.delete_user_pool, UserPoolId=pool_id)


@pytest.fixture
def second_pool_client(register_espuser_client):
    made = register_espuser_client(client_name="itest second pool app", redirect_uris=[SECOND_POOL_REDIRECT])
    return WebClient(made.client_id, SECOND_POOL_REDIRECT)


def test_a_second_pool_signs_in_on_our_page_and_on_its_own_hosted_page(
        second_pool, second_pool_client, chromium_browser):
    """The row naming password_verifier cognito is a pool our page takes the password for; the row of the same pool naming none is a button to its hosted UI; both end at the client as the same person."""
    person = second_pool.person
    on_our_page, on_hosted_page = chromium_browser.new_context(), chromium_browser.new_context()
    try:
        page = on_our_page.new_page()
        verifier, challenge = pkce_pair()
        page.goto(_authorize_url(second_pool_client.client_id, SECOND_POOL_REDIRECT, challenge, "state-pool"),
                  wait_until="load")
        page.wait_for_selector("#username", timeout=10000)
        methods = {p["name"]: p["method"] for p in json.loads(page.locator("#esp-page-data").text_content())["providers"]}
        assert methods.get(INBUILT_PROVIDER) == "password", methods
        assert methods.get(second_pool.cognito_row) == "password", methods
        assert methods.get(second_pool.oidc_row) == "redirect", methods
        assert page.get_by_role("link", name=f"Continue with {HOSTED_LABEL}", exact=True).is_visible()

        page.get_by_role("button", name=POOL_LABEL, exact=True).click()
        page.get_by_role("button", name=POOL_LABEL, exact=True, pressed=True).wait_for(timeout=5000)
        page.fill("#username", person.email)
        assert _submit_password(page, person.password, SECOND_POOL_REDIRECT) == second_pool.cognito_row, \
            "the password must go to the pool the person selected"
        signed_in_here = _redeem(second_pool_client, page, verifier, "state-pool")

        page = on_hosted_page.new_page()
        verifier, challenge = pkce_pair()
        page.goto(_authorize_url(second_pool_client.client_id, SECOND_POOL_REDIRECT, challenge, "state-hosted"),
                  wait_until="load")
        with page.expect_navigation(url=lambda u: urlparse(u).netloc == second_pool.hosted_host, timeout=30000):
            page.get_by_role("link", name=f"Continue with {HOSTED_LABEL}", exact=True).click()
        # The hosted page renders the form twice (desktop and mobile) with duplicate ids, so pick the visible one.
        page.locator("#signInFormUsername").locator("visible=true").first.fill(person.email)
        page.locator("#signInFormPassword").locator("visible=true").first.fill(person.password)
        with page.expect_navigation(url=lambda u: u.startswith(SECOND_POOL_REDIRECT), timeout=30000):
            page.locator("input[name=signInSubmitButton]").locator("visible=true").first.click()
        signed_in_there = _redeem(second_pool_client, page, verifier, "state-hosted")

        assert signed_in_there["sub"] == signed_in_here["sub"], "one person through either door of one pool is one account"
    finally:
        on_our_page.close()
        on_hosted_page.close()


def test_a_client_that_does_not_allow_the_second_pool_is_refused_before_its_password_is_checked(
        second_pool, register_espuser_client):
    """allowed_providers is enforced by POST /user/login/password, not only by which buttons the page draws, and before the pool sees the password: a wrong one still gets the 403."""
    made = register_espuser_client(client_name="itest built-in pool only", redirect_uris=[SECOND_POOL_REDIRECT],
                                   allowed_providers=[INBUILT_PROVIDER])
    browser = Browser()
    authz = WebClient(made.client_id, SECOND_POOL_REDIRECT).authorize(browser)
    assert authz.needs_login, authz
    refused = browser.session.post(f"{USER_API_GATEWAY_URL}/user/login/password", json={
        "flow_id": browser.flow_cookie, "provider": second_pool.cognito_row,
        "username": second_pool.person.email, "password": "not-" + second_pool.person.password,
    }, allow_redirects=False, timeout=30)
    assert refused.status_code == 403, refused.text
    assert refused.json().get("error") == "provider_not_allowed", refused.text
