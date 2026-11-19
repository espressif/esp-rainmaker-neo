# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Single sign-on, the device list, and sign-out: everything that is a statement about a
*session* rather than a single login.

Every assertion here spans at least two requests sharing a cookie jar, which is why none of
them can be a unit test. A cookie set by one request is honoured (or refused) by another,
through a real gateway against a real table -- and the by-user index the sessions API reaches
a device through is not modelled by the DynamoDB mock the unit specs run against, so a second
browser acting on the first is code the unit layer runs straight past. The protocol rules
themselves (does prompt=none answer silently? is an unparseable max_age a failure?) are pinned
in sso_test.go / sessions_main_test.go and deliberately not repeated.

The real-browser end to end -- a live Chromium accepting our __Host- cookies, the provider's
own end-session hop -- is test_user_login_ui.py. This file is the fast requests-level cover.

Spec: espuser/docs/specs/sso-sessions.md, authorize-code-flow.md.
"""
import time
import uuid
from types import SimpleNamespace
from urllib.parse import parse_qs, urljoin, urlparse

import pytest
import requests

from py_sdk.espuser_oauth import Browser, WebClient, client_ids_under, held_for_consent, sids_in
from test.itest.conftest import (
    API_GATEWAY_URL,
    ESPUSER_RESOURCE,
    NEO_RESOURCE,
    SESSION_COOKIE,
    SESSIONS_SCOPE,
    USER_API_GATEWAY_URL,
    decode_jwt_claims,
    generate_test_password,
    requires_espuser,
)
from test.itest.email_utils import (
    generate_mailosaur_email,
    get_verification_code_from_server,
)

pytestmark = [pytest.mark.espuser, requires_espuser]

# example.com resolves. That matters: if a redirect is ever followed by accident -- the upstream
# auto-completing a login it did not need to show a form for, say -- the request lands on a real
# host and fails as an assertion rather than as a DNS error three frames deep.
CALLBACK = "https://example.com/espuser-itest/{}/callback"
SIGNED_OUT = "https://example.com/espuser-itest/{}/signed-out"


@pytest.fixture
def apps(register_espuser_client):
    """Two products a person might have open at once, each with its own registration and a
    registered landing page for after a sign-out."""
    made = []
    for n in ("one", "two"):
        uri, bye = CALLBACK.format(n), SIGNED_OUT.format(n)
        client = register_espuser_client(redirect_uris=[uri], post_logout_redirect_uris=[bye])
        made.append(WebClient(client.client_id, uri, post_logout_redirect_uri=bye))
    return made


def signed_in(app, browser, person, **extra):
    """Sign in and return the token set, so a test reads as a sequence of logins."""
    return app.tokens(app.sign_in(browser, person, **extra))


def bearer_challenge(response):
    """The RFC 6750 challenge as it actually arrives at the client. API Gateway renames
    WWW-Authenticate to x-amzn-Remapped-WWW-Authenticate, so the standard name never reaches a
    caller; both are accepted, and the assertion stays on the thing that matters -- that the
    challenge reaches the client at all."""
    headers = {k.lower(): v for k, v in response.headers.items()}
    return headers.get("www-authenticate") or headers.get("x-amzn-remapped-www-authenticate") or ""


# ── the single sign-on promise, and the ways to refuse it (OIDC Core s3.1.2.1) ────


def test_sso_multi_webapps(apps, espuser_person):
    """One login, several web apps: sign in to one and the next is already signed in without
    another trip to the provider -- and only in the browser that logged in.

    `used_upstream` is the assertion that matters. Both logins succeeding proves nothing -- that
    would also happen with no session at all, because the provider still holds its own cookie. What
    must be true is that the SECOND client never left our own authorize endpoint, and that a
    DIFFERENT browser gets no such shortcut (a session that follows the person is not a session,
    and it is also what stops a stolen cookie from being a stolen account everywhere)."""
    first, second = apps
    browser = Browser()

    one = first.sign_in(browser, espuser_person)
    assert one.used_upstream, "the first login must actually authenticate somebody"
    assert one.code and browser.session_cookie, "a completed login must leave a session cookie"
    signed_in_at = decode_jwt_claims(first.tokens(one)["id_token"])["auth_time"]

    # auth_time has whole-second resolution; without the gap a code stamped with now would still match
    time.sleep(2)
    two = second.sign_in(browser, espuser_person)
    assert not two.used_upstream, \
        "the second web app must be answered from our own session, not a round trip upstream"
    second_tokens = second.tokens(two)
    assert second_tokens["access_token"], "and the silent code must spend"
    assert decode_jwt_claims(second_tokens["id_token"])["auth_time"] == signed_in_at, \
        "a code answered from the session authenticated nobody, so it must carry the session's auth_time"

    assert first.sign_in(Browser("phone"), espuser_person).used_upstream, \
        "a second device must authenticate; a session that follows the person is not a session"


def test_sso_prompt_login_always_re_authenticates(apps, espuser_person):
    """A client asking for a fresh login gets one, session or no session -- the parameter a bank's
    re-authentication step is built on, so 'usually honoured' is not a thing it can be."""
    first, _ = apps
    browser = Browser()
    assert first.sign_in(browser, espuser_person).used_upstream

    again = first.authorize(browser, prompt="login")
    assert again.needs_login and not again.code, \
        f"prompt=login must not be answered from the session: {again!r}"


def test_sso_prompt_none_answers_silently_or_fails(apps, espuser_person):
    """The parameter an app refreshes its tokens with in a hidden iframe: answer or fail, never
    show a page. No session is a login_required error the client can correlate by state, not a login
    page the user would see appear from nowhere; a live session is answered with a code."""
    first, second = apps

    refused = first.authorize(Browser(), prompt="none", state="silent-state")
    assert refused.error == "login_required", f"{refused!r}"
    assert refused.code is None
    assert refused.returned_state == "silent-state", \
        "the client correlates the answer by state; an error without it cannot be matched"

    browser = Browser()
    assert first.sign_in(browser, espuser_person).used_upstream
    assert second.authorize(browser, prompt="none").code, "a live session must answer with a code"


def test_sso_max_age_bounds_the_authentication_age(apps, espuser_person):
    """max_age bounds how old the AUTHENTICATION may be, not the session. Zero means 'just now', so
    a session established seconds ago must re-authenticate; a generous bound is satisfied by it --
    the positive control that stops the first half passing on a max_age that breaks the shortcut
    unconditionally."""
    first, second = apps
    browser = Browser()
    assert first.sign_in(browser, espuser_person).used_upstream

    assert first.authorize(browser, max_age="0").needs_login, "max_age=0 must re-authenticate"
    assert second.authorize(browser, max_age="86400").code, \
        "a session minutes old must satisfy max_age=86400"


def test_sso_prompt_login_and_max_age_reach_the_provider(apps, espuser_person, hosted_cognito_provider):
    """The half of prompt=login that is easy to get wrong and invisible when it is: the demand must
    reach the PROVIDER. If it does not, the provider re-authenticates silently from its own cookie
    and the person is waved through without typing anything -- it looks like a fresh login and is
    not one. Asserted on the URL we send the browser to, because the forwarding is our property and
    the provider's response is not."""
    first, _ = apps
    browser = Browser()

    started = first.authorize(browser, prompt="login", max_age="120")
    assert started.needs_login, started
    fed = first.start_federation(browser, hosted_cognito_provider)
    assert fed.status_code == 302, f"{fed.status_code} {fed.text[:200]}"
    upstream = fed.headers["Location"]
    assert "prompt=login" in upstream, \
        f"prompt=login did not reach the provider, so it means only 'skip our session': {upstream}"
    assert "max_age=120" in upstream, f"max_age did not reach the provider: {upstream}"



def _cancel_at_the_provider(browser, client, provider):
    """Start a sign-in, leave for the provider, and come back as a provider does when the person
    presses Cancel: our own callback with error=access_denied and the state we sent."""
    started = client.authorize(browser, state="cancel-state")
    assert started.needs_login, started
    fed = client.start_federation(browser, provider)
    assert fed.status_code == 302, f"{fed.status_code} {fed.text[:200]}"
    state = parse_qs(urlparse(fed.headers["Location"]).query)["state"][0]
    return state, lambda b: b.session.get(f"{USER_API_GATEWAY_URL}/oauth2/federation/callback",
                                          params={"error": "access_denied", "state": state}, allow_redirects=False)


def test_sso_a_cancel_at_the_provider_returns_to_our_page_when_another_way_in_exists(
        apps, hosted_cognito_provider):
    """The built-in pool still takes a password on our page, so backing out of the provider lands
    back on our sign-in page to choose again, with no code and no session."""
    first, _ = apps
    browser = Browser()
    _, come_back = _cancel_at_the_provider(browser, first, hosted_cognito_provider)
    back = come_back(browser)
    assert back.status_code == 302, f"{back.status_code} {back.text[:200]}"
    assert urlparse(back.headers["Location"]).path.endswith("/user/login"), back.headers["Location"]
    assert browser.session_cookie is None, "a cancelled sign-in must not leave a session"


def test_sso_a_cancel_at_the_only_provider_tells_the_app_access_denied(
        register_espuser_client, hosted_cognito_provider):
    """A client that allows only that provider has nothing else to offer, so the app is told the
    person declined (RFC 6749 s4.1.2.1), with its own state."""
    uri = CALLBACK.format(uuid.uuid4().hex[:8])
    client = register_espuser_client(redirect_uris=[uri], allowed_providers=[hosted_cognito_provider])
    app = WebClient(client.client_id, uri)
    browser = Browser()
    _, come_back = _cancel_at_the_provider(browser, app, hosted_cognito_provider)
    back = come_back(browser)
    assert back.status_code == 302, f"{back.status_code} {back.text[:200]}"
    returned = urlparse(back.headers["Location"])
    assert back.headers["Location"].startswith(uri), back.headers["Location"]
    assert parse_qs(returned.query) == {"error": ["access_denied"], "state": ["cancel-state"]}, returned.query


def test_sso_a_cancel_from_another_browser_ends_nobody_s_sign_in(apps, hosted_cognito_provider):
    """The same cancel without the flow cookie is refused on our own page: a stranger holding a
    leaked state cannot end someone else's sign-in or send their app access_denied."""
    first, _ = apps
    _, come_back = _cancel_at_the_provider(Browser(), first, hosted_cognito_provider)
    stranger = come_back(Browser("another browser"))
    assert stranger.status_code == 400, f"{stranger.status_code} {stranger.text[:200]}"
    assert "Location" not in stranger.headers, stranger.headers

# ── session fixation: a value the attacker chose ──────────────────────────────────


def test_sso_a_session_cookie_the_attacker_controls_is_worthless(apps, espuser_person):
    """Two ways an attacker gets a value into the jar, and neither buys a session.

    Fixation: a value the attacker planted must resolve to nothing, and the login that follows must
    mint a value the attacker never saw. Replay of a spent value: a cookie that WAS valid must stop
    working once its row is gone -- nothing about the bytes distinguishes the two, which is the
    point; the secret is worthless without the record it names."""
    first, _ = apps
    browser = Browser()
    planted = "attacker-chosen-session-value"
    browser.plant_session_cookie(planted)
    assert first.authorize(browser, prompt="none").error == "login_required", \
        "a cookie value we never issued must resolve to no session"

    done = first.sign_in(browser, espuser_person)
    assert done.used_upstream and done.code
    assert browser.session_cookie != planted, \
        "the login adopted the attacker's value; whoever planted it now holds the victim's session"

    stolen = browser.session_cookie
    first.logout(browser)
    thief = Browser()
    thief.plant_session_cookie(stolen)
    assert first.authorize(thief, prompt="none").error == "login_required", \
        "a signed-out cookie still opened a session"


def test_sso_re_authenticating_retires_the_previous_cookie(apps, espuser_person):
    """Signing in again is what a person does when they think their session was stolen; it has to
    mean something. The old value must stop working, and the browser must still be ONE browser
    afterwards -- the sid is carried across so every product already signed in stays attached to it
    and one sign-out still reaches them all."""
    first, second = apps
    browser = Browser()
    assert first.sign_in(browser, espuser_person).used_upstream
    old_cookie = browser.session_cookie

    tokens = first.tokens(first.authorize(browser))
    sid_before = [s["session_id"] for s in WebClient.sessions(tokens["access_token"]).json()["sessions"]
                  if s["current"]][0]

    again = first.sign_in(browser, espuser_person, prompt="login")
    assert again.used_upstream and again.code
    assert browser.session_cookie != old_cookie, "a re-authentication must mint a new secret"

    thief = Browser()
    thief.plant_session_cookie(old_cookie)
    assert second.authorize(thief, prompt="none").error == "login_required", \
        "the cookie held before the re-login still works, so signing in again bought nothing"

    after = WebClient.sessions(first.tokens(again)["access_token"]).json()
    assert len(after["sessions"]) == 1, \
        f"one browser must stay one row; two logins left {len(after['sessions'])}: {after}"
    assert after["sessions"][0]["session_id"] == sid_before, \
        "the session kept its identity across the re-login, or every product signed in before it " \
        "would be orphaned from the browser it belongs to"


# ── code interception: a code the attacker holds ─────────────────────────────────


def test_sso_a_code_is_bound_to_its_client_and_redirect_uri(apps, espuser_person):
    """An authorization code is bound to the client it was issued to and the redirect_uri it was
    issued for. Without the first, a code intercepted in a redirect is spendable by whoever holds
    it; without the second (RFC 6749 s4.1.3), a code obtained through one registered URI is spent as
    though it arrived at another. The rightful spend still works: the refusals are about who asked
    and where, not the code itself."""
    first, second = apps
    browser = Browser()
    issued = first.sign_in(browser, espuser_person)
    assert issued.code

    stolen = second.exchange_code(issued.code, issued.verifier)
    assert stolen.status_code in (400, 401), f"{stolen.status_code} {stolen.text[:200]}"
    assert stolen.json().get("error") in ("invalid_grant", "invalid_client"), stolen.text

    mismatched = first.exchange_code(issued.code, issued.verifier,
                                     redirect_uri="https://elsewhere.itest.example/callback")
    assert mismatched.status_code in (400, 401), f"{mismatched.status_code} {mismatched.text[:200]}"
    assert mismatched.json().get("error") == "invalid_grant", mismatched.text

    assert first.exchange(issued).status_code == 200, "the rightful client must still spend it"


def test_sso_a_code_is_single_use(apps, espuser_person):
    """Replay. The rule is unit-tested; what is asserted here is that two requests seconds apart
    through a real gateway and a real table cannot both succeed -- a property of the storage, not of
    the branch."""
    first, _ = apps
    issued = first.sign_in(Browser(), espuser_person)
    assert first.exchange(issued).status_code == 200
    replayed = first.exchange(issued)
    assert replayed.status_code in (400, 401), f"{replayed.status_code} {replayed.text[:200]}"


def test_sso_the_shortcut_still_honours_the_registered_redirect_uri(register_espuser_client, apps, espuser_person):
    """The SSO path issues a code without a login page, so it is the path where a skipped
    redirect_uri check would go unnoticed. An unregistered URI must be refused on our own page,
    before any code exists -- never by redirecting to it. Likewise a client with no
    default_resource that names no resource gets invalid_target (RFC 8707 s2), never a code."""
    first, second = apps
    browser = Browser()
    assert first.sign_in(browser, espuser_person).used_upstream

    rogue = WebClient(second.client_id, "https://attacker.itest.example/callback",
                      scope=f"openid email {SESSIONS_SCOPE}")
    refused = rogue.authorize(browser)
    assert refused.code is None, "a live session must not excuse an unregistered redirect_uri"
    assert refused.response.status_code == 400, \
        f"an unregistered redirect_uri must fail on our page, never by redirecting to it: {refused!r}"

    uri = CALLBACK.format("no-default")
    no_default = WebClient(register_espuser_client(redirect_uris=[uri], default_resource="").client_id, uri)
    unnamed = no_default.authorize(browser)
    assert unnamed.error == "invalid_target", f"expected invalid_target, got {unnamed!r}"
    assert unnamed.code is None, "a live session must not excuse a sign-in that covers no resource"


# ── the device list ───────────────────────────────────────────────────────────────


def test_sso_multi_browser(apps, espuser_person):
    """The device list is the shape of the thing. You do not sign out of an application, you sign
    out of a browser, and everything opened in it goes with it -- so products are nested under a
    session, not listed beside it. A second device is a second row, and finding it goes through the
    by-user index a mock cannot model: the phone's token has to reach the laptop's session, whose
    cookie hash is not derivable from anything the phone holds."""
    first, second = apps
    laptop, phone = Browser("laptop"), Browser("phone")
    laptop_tokens = signed_in(first, laptop, espuser_person)
    signed_in(second, laptop, espuser_person)

    body = WebClient.sessions(laptop_tokens["access_token"]).json()
    assert len(body["sessions"]) == 1, f"two products in one browser is one device: {body}"
    view = body["sessions"][0]
    assert view["current"] is True, \
        "the browser making the request must be labelled, or a UI cannot warn before ending it"
    assert view["signed_in_at"] == decode_jwt_claims(laptop_tokens["id_token"])["auth_time"], \
        "the session list and the ID token must name the same authentication"
    assert client_ids_under(view) == sorted([first.client_id, second.client_id]), view

    phone_tokens = signed_in(first, phone, espuser_person)
    two = WebClient.sessions(phone_tokens["access_token"]).json()
    assert len(two["sessions"]) == 2, f"two devices, two rows: {two}"
    assert len([s for s in two["sessions"] if s["current"]]) == 1, \
        f"exactly one row is the browser asking: {two}"


def test_sso_the_list_shows_only_the_callers_own_devices(apps, espuser_person, second_espuser_person):
    """Two people signed in on two browsers. Neither may see the other's -- the index is queried
    per user, and a shared sid would mean it is not."""
    first, _ = apps
    mine, theirs = Browser("mine"), Browser("theirs")
    my_tokens = signed_in(first, mine, espuser_person)
    their_tokens = signed_in(first, theirs, second_espuser_person)

    my_sids = sids_in(WebClient.sessions(my_tokens["access_token"]).json())
    their_sids = sids_in(WebClient.sessions(their_tokens["access_token"]).json())
    assert my_sids and their_sids
    assert not set(my_sids) & set(their_sids), \
        f"a shared sid means the list is not scoped to the caller: {my_sids} {their_sids}"


def test_sso_end_session_the_other_device_is_untouched(apps, espuser_person):
    """Sign the laptop out from the phone -- the lost-device case, and the reason this endpoint
    exists at all. The ended device cannot get new tokens; the device that did the ending is
    untouched; and the ended row is gone from the list."""
    first, _ = apps
    laptop, phone = Browser("laptop"), Browser("phone")
    laptop_tokens = signed_in(first, laptop, espuser_person)
    phone_tokens = signed_in(first, phone, espuser_person)

    laptop_sid = [s["session_id"] for s in WebClient.sessions(phone_tokens["access_token"]).json()["sessions"]
                  if not s["current"]][0]
    ended = WebClient.end_session(phone_tokens["access_token"], laptop_sid)
    assert ended.status_code == 204, f"{ended.status_code} {ended.text[:200]}"

    assert not first.still_signed_in(laptop_tokens["refresh_token"]), \
        "the ended device must not be able to get new tokens"
    assert first.still_signed_in(phone_tokens["refresh_token"]), \
        "and the device that did the ending must be untouched"
    assert laptop_sid not in sids_in(WebClient.sessions(phone_tokens["access_token"]).json())


def test_sso_end_all_sessions(apps, espuser_person):
    """"Everywhere" means everywhere, the asking browser included -- signing someone out of every
    device except the one they are on would be a different feature wearing this one's name. What is
    left is honest: an access token is a stateless signed JWT nothing server-side can withdraw, so
    the token that asked keeps authenticating until it expires; it just finds nothing left to see."""
    first, second = apps
    laptop, phone = Browser("laptop"), Browser("phone")
    laptop_first = signed_in(first, laptop, espuser_person)
    laptop_second = signed_in(second, laptop, espuser_person)
    phone_tokens = signed_in(first, phone, espuser_person)

    ended = WebClient.end_all_sessions(phone_tokens["access_token"])
    assert ended.status_code == 204, f"{ended.status_code} {ended.text[:200]}"

    assert not first.still_signed_in(laptop_first["refresh_token"])
    assert not second.still_signed_in(laptop_second["refresh_token"]), \
        "every product on the other device, not just the one that happened to be listed first"
    assert not first.still_signed_in(phone_tokens["refresh_token"]), "and the asking browser goes too"

    left = WebClient.sessions(phone_tokens["access_token"])
    assert left.status_code == 200, f"{left.status_code} {left.text[:200]}"
    assert left.json()["sessions"] == [], f"every device must be gone: {left.json()}"
    assert not left.json().get("unattached_products"), \
        f"and no standing access may survive as an orphan: {left.json()}"


def test_sso_another_persons_session_id_is_a_404_not_a_403(apps, espuser_person, second_espuser_person):
    """The oracle test. A 403 would confirm the sid exists; a 404 says only "not yours", and the
    two must be indistinguishable to someone guessing. This runs through the real by-user index --
    the delete path re-resolves a sid to a row through it precisely so a client never hands back an
    internal key."""
    first, _ = apps
    mine, theirs = Browser("mine"), Browser("theirs")
    my_tokens = signed_in(first, mine, espuser_person)
    their_tokens = signed_in(first, theirs, second_espuser_person)

    their_sid = sids_in(WebClient.sessions(their_tokens["access_token"]).json())[0]
    stolen = WebClient.end_session(my_tokens["access_token"], their_sid)
    assert stolen.status_code == 404, \
        f"another person's sid must be a 404, got {stolen.status_code}: {stolen.text[:200]}"
    made_up = WebClient.end_session(my_tokens["access_token"], "sid-that-never-existed")
    assert made_up.status_code == stolen.status_code, \
        "a real sid belonging to someone else and a made-up one must answer identically"
    assert first.still_signed_in(their_tokens["refresh_token"]), \
        "and of course it must not actually have ended their session"


def test_sso_sessions_endpoint_requires_the_sessions_scope(register_espuser_client, espuser_person):
    """Being able to sign somebody in is not a reason to enumerate every device they use, so the
    device list has a scope of its own -- and it must reject anything that is not an access token
    bearing it. A token without account:sessions is 403 with a challenge that names the scope
    (RFC 6750 s3); a missing token is 401 with a Bearer challenge; an id token -- validly signed by
    this same issuer but audienced to the client, not this API -- must be 401 too, or every client's
    own token becomes a key here (RFC 9700 token substitution).
    One sign-in may cover neo as well (RFC 8707); the access token narrowed to neo is accepted here, since the account's own endpoints take any of our APIs' tokens."""
    uri = CALLBACK.format("noscope")
    client = register_espuser_client(redirect_uris=[uri], scopes=["openid", "email"])
    app = WebClient(client.client_id, uri, scope="openid email")
    noscope = signed_in(app, Browser(), espuser_person)

    refused = WebClient.sessions(noscope["access_token"])
    assert refused.status_code == 403, f"{refused.status_code} {refused.text[:200]}"
    assert refused.json().get("error") == "insufficient_scope", refused.text
    challenge = bearer_challenge(refused)
    assert SESSIONS_SCOPE in challenge and 'error="insufficient_scope"' in challenge, \
        f"the challenge must name the scope that would have worked, or a client cannot recover: {challenge!r}"
    assert WebClient.end_all_sessions(noscope["access_token"]).status_code == 403, \
        "and the same gate on the destructive verb, which is the one that matters"

    two_apis_uri = CALLBACK.format("two-apis")
    two_apis = WebClient(register_espuser_client(redirect_uris=[two_apis_uri]).client_id, two_apis_uri,
                         resource=[ESPUSER_RESOURCE, NEO_RESOURCE])
    tokens = signed_in(two_apis, Browser(), espuser_person)
    assert decode_jwt_claims(tokens["access_token"])["aud"] == ESPUSER_RESOURCE, "the client's default"
    assert WebClient.sessions(tokens["access_token"]).status_code == 200
    missing = requests.get(f"{USER_API_GATEWAY_URL}/v1/user/sessions", timeout=30)
    assert missing.status_code == 401 and "Bearer" in bearer_challenge(missing), \
        f"a 401 with no challenge tells a client nothing about how to authenticate: {dict(missing.headers)}"
    substituted = WebClient.sessions(tokens["id_token"])
    assert substituted.status_code == 401, \
        "an id token is audienced to the client, not this API; accepting one here would make " \
        f"every client's own token a key to this endpoint: {substituted.text[:200]}"

    neo = two_apis.refresh(tokens["refresh_token"], resource=NEO_RESOURCE)
    assert neo.status_code == 200, f"narrowing refresh: {neo.status_code} {neo.text[:300]}"
    assert decode_jwt_claims(neo.json()["access_token"])["aud"] == NEO_RESOURCE
    assert WebClient.sessions(neo.json()["access_token"]).status_code == 200, "the ESP User API takes a token addressed to any of our APIs"


def test_sso_rotated_refresh_token_still_covers_every_api_of_the_sign_in(register_espuser_client, espuser_person):
    """Rotation renews an authorization, never narrows it (RFC 8707 s2.2): after two rotations the refresh token still gets a token for each API the authorize request named, and neo refuses the ESP User API's with 401 (RFC 9068 s4) while the ESP User API takes either."""
    uri = CALLBACK.format("rotated")
    app = WebClient(register_espuser_client(redirect_uris=[uri]).client_id, uri, resource=[ESPUSER_RESOURCE, NEO_RESOURCE])
    refresh_token = signed_in(app, Browser(), espuser_person)["refresh_token"]
    for _ in range(2):
        rotated = app.refresh(refresh_token)
        assert rotated.status_code == 200, f"rotation: {rotated.status_code} {rotated.text[:300]}"
        assert rotated.json()["refresh_token"] != refresh_token, "every redemption must rotate"
        refresh_token = rotated.json()["refresh_token"]

    espuser = app.refresh(refresh_token, resource=ESPUSER_RESOURCE)
    assert espuser.status_code == 200, f"the rotated token must still cover the ESP User API: {espuser.text[:300]}"
    neo = app.refresh(espuser.json()["refresh_token"], resource=NEO_RESOURCE)
    assert neo.status_code == 200, f"and still cover neo after narrowing to the ESP User API: {neo.text[:300]}"
    espuser_tokens, neo_tokens = espuser.json(), neo.json()
    assert decode_jwt_claims(espuser_tokens["access_token"])["aud"] == ESPUSER_RESOURCE
    assert decode_jwt_claims(neo_tokens["access_token"])["aud"] == NEO_RESOURCE

    assert WebClient.sessions(espuser_tokens["access_token"]).status_code == 200
    assert WebClient.sessions(neo_tokens["access_token"]).status_code == 200, "the ESP User API takes a token addressed to any of our APIs"
    creds_url = f"{API_GATEWAY_URL}/v1/user/credentials"
    refused = requests.post(creds_url, headers={"Authorization": f"Bearer {espuser_tokens['access_token']}"},
                            json={"id_token": espuser_tokens["id_token"]}, timeout=30)
    assert refused.status_code == 401, f"neo must refuse a token addressed to the ESP User API: {refused.text[:200]}"
    # An itest client is not one of neo's first-party apps, so its neo token clears the audience check and only the client binding refuses it.
    own = requests.post(creds_url, headers={"Authorization": f"Bearer {neo_tokens['access_token']}"},
                        json={"id_token": neo_tokens["id_token"]}, timeout=30)
    assert own.status_code == 403, f"neo must accept the audience of its own token: {own.status_code} {own.text[:200]}"


# ── RP-initiated sign-out ─────────────────────────────────────────────────────────


def test_sso_logout_ends_every_product_and_clears_the_cookie(apps, espuser_person):
    """One click, and everything the browser was signed in to is signed out -- asserted on the
    refresh rather than the response code, because a logout that returns 302 and deletes nothing is
    indistinguishable from a working one at the HTTP level.

    And the session cookie, read from the raw Set-Cookie header at both ends because its attributes
    exist only on the wire. A __Host- cookie whose attributes are wrong is dropped by the browser
    WITHOUT SAYING SO, so single sign-on would just stop working with a green server: the login must
    set it Secure, HttpOnly, Path=/, SameSite=Lax and Domain-less, and the sign-out must clear it now
    with Max-Age=0 rather than leave a credential that outlives its row. The live-Chromium version --
    a real browser ACCEPTING these attributes, the provider's own end-session hop -- is
    test_user_login_ui.py; this is the fast requests-level check."""
    first, second = apps
    browser = Browser()
    one = signed_in(first, browser, espuser_person)
    two = signed_in(second, browser, espuser_person)

    login_cookie = browser.last_session_set_cookie
    assert login_cookie and login_cookie.startswith(SESSION_COOKIE + "="), login_cookie
    assert SESSION_COOKIE.startswith("__Host-"), SESSION_COOKIE
    for attr in ("Path=/", "Secure", "HttpOnly", "SameSite=Lax"):
        assert attr in login_cookie, f"the __Host- session cookie is missing {attr}: {login_cookie}"
    assert "domain=" not in login_cookie.lower(), \
        f"a Domain attribute voids the prefix and the browser refuses the cookie: {login_cookie}"
    assert browser.session_cookie, "and the value must be one a cookie jar accepts"

    assert first.still_signed_in(one["refresh_token"]) and second.still_signed_in(two["refresh_token"]), \
        "precondition"
    pending = first.sign_in(browser, espuser_person)
    assert not pending.used_upstream and pending.code, "a code answered from the session, left unredeemed"

    out = second.logout(browser, id_token_hint=two.get("id_token"))
    assert out.status_code in (200, 302), f"{out.status_code} {out.text[:200]}"
    headers = {k.lower(): v for k, v in out.headers.items()}
    assert "no-store" in headers.get("cache-control", ""), \
        f"a cached sign-out is a sign-out that stops happening: {headers.get('cache-control')}"
    if out.status_code == 302:
        assert headers.get("referrer-policy") == "no-referrer", \
            f"the logout URL can carry an id_token_hint; the destination must not get it: {headers.get('referrer-policy')}"

    assert not second.still_signed_in(two["refresh_token"]), "the product that asked must be signed out"
    assert not first.still_signed_in(one["refresh_token"]), \
        "and so must every other product in the same browser; one refresh family dying alone means " \
        "the calling app revoked its own token and nothing else happened"

    clear = browser.last_session_set_cookie
    assert clear and clear.startswith(SESSION_COOKIE + "="), clear
    assert "Max-Age=0" in clear, f"the sign-out did not clear the cookie: {clear}"
    assert "Path=/" in clear and "Secure" in clear, \
        f"the clearing cookie must carry the same attributes or the browser keeps both: {clear}"
    assert browser.session_cookie is None, "the jar still holds a session cookie after sign-out"
    assert first.authorize(browser, prompt="none").error == "login_required", \
        "the session row survived the sign-out; the next authorize would shortcut to a code"

    late = first.tokens(pending)
    assert not first.still_signed_in(late["refresh_token"]), \
        "a code redeemed after sign-out may mint once, but its refresh token must die at the first refresh"


def test_sso_logout_accepts_a_form_post(apps, espuser_person):
    """RP-Initiated Logout 1.0 §2 requires POST as well as GET. A cross-site POST carries no
    SameSite=Lax cookie, so the POST is answered with a 303 to the same parameters as a GET and ends
    nothing itself; following that 303 in the same browser is what signs the person out."""
    first, _ = apps
    browser = Browser()
    tokens = signed_in(first, browser, espuser_person)

    posted = browser.session.post(f"{USER_API_GATEWAY_URL}/oauth2/logout", data={
        "client_id": first.client_id, "id_token_hint": tokens["id_token"],
    }, allow_redirects=False)
    assert posted.status_code == 303, f"{posted.status_code} {posted.text[:200]}"
    assert "no-store" in posted.headers.get("Cache-Control", ""), posted.headers

    out = browser.session.get(urljoin(posted.url, posted.headers["Location"]), allow_redirects=False)
    assert out.status_code in (200, 302), f"{out.status_code} {out.text[:200]}"
    assert not first.still_signed_in(tokens["refresh_token"]), \
        "following the 303 must end the session and every family under it"


def test_sso_logout_ignores_an_unregistered_landing_page_but_still_signs_out(apps, espuser_person):
    """An endpoint that forwards a browser anywhere on a caller's say-so is an open redirect wearing
    the issuer's hostname -- the most credible phishing origin this deployment owns. It must refuse
    the unregistered URL and still SIGN THE PERSON OUT: refusing to forward is safe, refusing to sign
    out over a bad return URL would leave the session alive for a cosmetic reason."""
    first, _ = apps
    browser = Browser()
    tokens = signed_in(first, browser, espuser_person)

    out = first.logout(browser, post_logout_redirect_uri="https://attacker.example/harvest")
    assert "attacker.example" not in out.headers.get("Location", ""), \
        f"open redirect: {out.headers.get('Location')}"
    assert not first.still_signed_in(tokens["refresh_token"]), \
        "the sign-out itself must have happened regardless of the rejected return URL"


def test_sso_logout_an_unverifiable_hint_does_not_block(apps, espuser_person):
    """A hint that does not verify must not veto the sign-out. Refusing on an unverifiable hint buys
    nothing -- a caller wanting no hint checked simply omits it -- and costs every genuine sign-out
    whose id_token has merely aged past expiry. The cookie is the credential; the hint only narrows.
    (Expiry tolerance is asserted in logout_test.go, which can control the clock; this is the
    deployed endpoint's behaviour for a hint it cannot make sense of at all.)"""
    first, _ = apps
    browser = Browser()
    tokens = signed_in(first, browser, espuser_person)

    out = first.logout(browser, id_token_hint="not.a.valid.jwt")
    assert out.status_code in (200, 302), f"{out.status_code} {out.text[:200]}"
    assert not first.still_signed_in(tokens["refresh_token"]), \
        "an unverifiable hint stopped the sign-out, making it a silent no-op for any aged id_token"


def test_sso_logout_a_hint_naming_someone_else_ends_nothing(apps, espuser_person, second_espuser_person):
    """The one case a veto is for. OIDC says the hint identifies the session to end; ending a
    DIFFERENT person's session on a stranger's say-so is the outcome that must not happen. Their id
    token, my cookie -- nothing may be destroyed."""
    first, _ = apps
    mine, theirs = Browser("mine"), Browser("theirs")
    my_tokens = signed_in(first, mine, espuser_person)
    their_tokens = signed_in(first, theirs, second_espuser_person)

    out = first.logout(mine, id_token_hint=their_tokens["id_token"])
    assert out.status_code in (200, 302), f"{out.status_code} {out.text[:200]}"
    assert first.still_signed_in(my_tokens["refresh_token"]), \
        "a hint naming another subject must end nothing, not end mine"
    assert first.still_signed_in(their_tokens["refresh_token"]), \
        "and certainly not end theirs -- I hold no credential of theirs"


# ── offline_access: a grant no sign-out ends (OIDC Core 1.0 s11) ──────────────────

OFFLINE_SCOPE = f"openid email {SESSIONS_SCOPE} offline_access"


def offline_client(register_espuser_client, name):
    """A first-party product registered for offline_access, asking for it on every sign-in."""
    uri = CALLBACK.format(name)
    client = register_espuser_client(redirect_uris=[uri],
                                      scopes=["openid", "email", "profile", SESSIONS_SCOPE, "offline_access"])
    return WebClient(client.client_id, uri, scope=OFFLINE_SCOPE)


def test_sso_an_offline_grant_outlives_logout_and_sign_out_everywhere(apps, register_espuser_client, espuser_person):
    """An offline grant belongs to no session (OIDC Back-Channel Logout 1.0 s2.7), so logout and sign out everywhere leave it refreshing, with no sid in anything it is issued."""
    plain, _ = apps
    offline = offline_client(register_espuser_client, "offline")
    browser = Browser()
    session_tokens = signed_in(plain, browser, espuser_person)
    offline_tokens = offline.tokens(offline.sign_in_allowing(browser, espuser_person))
    assert "sid" not in decode_jwt_claims(offline_tokens["id_token"]), "an offline grant belongs to no session"

    out = plain.logout(browser, id_token_hint=session_tokens["id_token"])
    assert not plain.still_signed_in(session_tokens["refresh_token"]), "the session's own product is signed out"

    renewed = offline.refresh(offline_tokens["refresh_token"])
    assert renewed.status_code == 200, f"the offline grant must survive the logout: {renewed.status_code} {renewed.text[:200]}"
    for token in ("access_token", "id_token"):
        assert "sid" not in decode_jwt_claims(renewed.json()[token]), f"a renewed {token} names no session either"

    again = signed_in(plain, browser, espuser_person)
    ended = WebClient.end_all_sessions(again["access_token"])
    assert ended.status_code == 204, f"{ended.status_code} {ended.text[:200]}"
    assert not plain.still_signed_in(again["refresh_token"])
    assert offline.still_signed_in(renewed.json()["refresh_token"]), "sign out everywhere leaves the offline grant"


def test_sso_offline_access_always_asks_for_consent(register_espuser_client, espuser_person):
    """Even an app we ship must ask for consent before an offline grant (OIDC Core 1.0 s11), so prompt=none gets consent_required."""
    offline = offline_client(register_espuser_client, "offline-consent")
    ordinary = WebClient(offline.client_id, offline.redirect_uri)
    browser = Browser()
    signed_in(ordinary, browser, espuser_person)

    assert ordinary.authorize(browser).code, "without offline_access the session answers with a code"

    held = offline.authorize(browser)
    assert held_for_consent(held), f"offline_access goes to the consent screen: {held!r}"
    assert held.code is None

    silent = offline.authorize(browser, prompt="none")
    assert silent.error == "consent_required", f"{silent!r}"


def connected_client_ids(access_token):
    listed = WebClient.connected_apps(access_token)
    assert listed.status_code == 200, f"{listed.status_code} {listed.text[:200]}"
    return [app["client_id"] for app in listed.json()["connected_apps"]]


def test_sso_connected_apps_lists_and_disconnects_an_offline_grant(apps, register_espuser_client, espuser_person):
    """An offline grant is listed as a connected app rather than under a session, and disconnecting the app ends every login it holds."""
    plain, _ = apps
    offline = offline_client(register_espuser_client, "connected")
    browser = Browser()
    session_tokens = signed_in(plain, browser, espuser_person)
    offline_tokens = offline.tokens(offline.sign_in_allowing(browser, espuser_person))
    manager = session_tokens["access_token"]

    ordinary = WebClient(offline.client_id, offline.redirect_uri)
    ordinary_tokens = signed_in(ordinary, browser, espuser_person)
    assert offline.client_id in connected_client_ids(manager), "the offline grant is a connected app"
    for view in WebClient.sessions(manager).json()["sessions"]:
        assert offline.client_id not in client_ids_under(view), f"an offline grant belongs to no session: {view}"

    ended = WebClient.disconnect_app(manager, offline.client_id)
    assert ended.status_code == 204, f"{ended.status_code} {ended.text[:200]}"
    assert not offline.still_signed_in(offline_tokens["refresh_token"]), "a disconnected app can no longer refresh"
    assert not ordinary.still_signed_in(ordinary_tokens["refresh_token"]), "its session login ends with it"
    assert offline.client_id not in connected_client_ids(manager)
    assert plain.still_signed_in(session_tokens["refresh_token"]), "the session that asked is untouched"

    again = WebClient.disconnect_app(manager, offline.client_id)
    assert again.status_code == 404, f"nothing left to disconnect: {again.status_code} {again.text[:200]}"


def test_sso_connected_apps_refuse_other_people_and_tokens_without_the_scope(
        apps, register_espuser_client, espuser_person, second_espuser_person):
    """Another person's connected app is a 404, never theirs to end, and a token without account:sessions is refused with a challenge naming the scope (RFC 6750 s3.1)."""
    plain, _ = apps
    offline = offline_client(register_espuser_client, "connected-guarded")
    offline_tokens = offline.tokens(offline.sign_in_allowing(Browser("mine"), espuser_person))

    theirs = signed_in(plain, Browser("theirs"), second_espuser_person)
    stolen = WebClient.disconnect_app(theirs["access_token"], offline.client_id)
    assert stolen.status_code == 404, f"another person's connected app must be a 404: {stolen.status_code} {stolen.text[:200]}"

    uri = CALLBACK.format("connected-noscope")
    noscope = WebClient(register_espuser_client(redirect_uris=[uri], scopes=["openid", "email"]).client_id, uri,
                        scope="openid email")
    unscoped = signed_in(noscope, Browser("noscope"), espuser_person)["access_token"]
    for refused in (WebClient.connected_apps(unscoped), WebClient.disconnect_app(unscoped, offline.client_id)):
        assert refused.status_code == 403, f"{refused.status_code} {refused.text[:200]}"
        assert SESSIONS_SCOPE in bearer_challenge(refused), f"the challenge names the scope: {bearer_challenge(refused)!r}"

    assert offline.still_signed_in(offline_tokens["refresh_token"]), "neither refusal ended the grant"


def test_sso_a_password_change_ends_every_session_and_connected_app(apps, register_espuser_client, espuser_person):
    """A changed password signs out everywhere, the session that changed it included, and disconnects every connected app, so the person signs in again with the new password."""
    plain, _ = apps
    offline = offline_client(register_espuser_client, "offline-password")
    caller = requests.post(f"{USER_API_GATEWAY_URL}/v1/user/auth/token", timeout=30,
                           json={"username": espuser_person.email, "password": espuser_person.password})
    assert caller.status_code == 200, f"{caller.status_code} {caller.text[:200]}"
    browser = Browser()
    browser_tokens = signed_in(plain, browser, espuser_person)
    offline_tokens = offline.tokens(offline.sign_in_allowing(browser, espuser_person))

    new_password = generate_test_password()
    changed = requests.post(f"{USER_API_GATEWAY_URL}/v1/user/auth/password", timeout=30, json={
        "access_token": caller.json()["access_token"], "old_password": espuser_person.password,
        "new_password": new_password,
    })
    assert changed.status_code == 200, f"{changed.status_code} {changed.text[:200]}"

    renewed = requests.post(f"{USER_API_GATEWAY_URL}/v1/user/auth/token/refresh", timeout=30,
                            json={"refresh_token": caller.json()["refresh_token"]})
    assert renewed.status_code != 200, f"the caller is signed out too: {renewed.status_code} {renewed.text[:200]}"
    assert not plain.still_signed_in(browser_tokens["refresh_token"]), "the browser's login is signed out"
    assert plain.authorize(browser).needs_login, "and the browser's session is gone, so it must sign in again"
    assert not offline.still_signed_in(offline_tokens["refresh_token"]), "the connected app is disconnected"
    again = requests.post(f"{USER_API_GATEWAY_URL}/v1/user/auth/token", timeout=30,
                          json={"username": espuser_person.email, "password": new_password})
    assert again.status_code == 200, f"the new password signs in: {again.status_code} {again.text[:200]}"


@pytest.mark.cognito_mail
def test_sso_a_password_reset_ends_every_session_and_keeps_connected_apps(
        apps, register_espuser_client, provision_end_user, require_mailosaur):
    """A reset signs out every browser and app, and a connected app stays, because forgetting a password says nothing about who else holds it."""
    email = generate_mailosaur_email()
    if not email:
        pytest.skip("Mailosaur address unavailable")
    password = generate_test_password()
    provision_end_user(email, {"email": email, "email_verified": "true"}, password)
    person = SimpleNamespace(email=email, password=password)
    plain, _ = apps
    offline = offline_client(register_espuser_client, "offline-reset")
    browser = Browser()
    browser_tokens = signed_in(plain, browser, person)
    offline_tokens = offline.tokens(offline.sign_in_allowing(browser, person))

    started = time.time()
    asked = requests.post(f"{USER_API_GATEWAY_URL}/v1/user/auth/password-recovery", timeout=30, json={"username": email})
    assert asked.status_code == 200, f"{asked.status_code} {asked.text[:200]}"
    code = get_verification_code_from_server(recipient_email=email, since_timestamp=started)
    assert code, "no recovery code arrived"
    reset = requests.post(f"{USER_API_GATEWAY_URL}/v1/user/auth/password-recovery/confirmation", timeout=30,
                          json={"username": email, "code": code, "new_password": generate_test_password()})
    assert reset.status_code == 200, f"{reset.status_code} {reset.text[:200]}"

    assert not plain.still_signed_in(browser_tokens["refresh_token"]), "the browser's login is signed out"
    assert offline.still_signed_in(offline_tokens["refresh_token"]), "the connected app stays"
