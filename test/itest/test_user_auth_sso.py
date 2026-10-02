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
import pytest
import requests

from test.itest.conftest import (
    SESSION_COOKIE,
    SESSIONS_SCOPE,
    UPSTREAM_PROVIDER,
    USER_API_GATEWAY_URL,
    requires_espuser,
)
from py_sdk.espuser_oauth import Browser, WebClient, client_ids_under, sids_in

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

    two = second.sign_in(browser, espuser_person)
    assert not two.used_upstream, \
        "the second web app must be answered from our own session, not a round trip upstream"
    assert second.tokens(two)["access_token"], "and the silent code must spend"

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


def test_sso_prompt_login_and_max_age_reach_the_provider(apps, espuser_person):
    """The half of prompt=login that is easy to get wrong and invisible when it is: the demand must
    reach the PROVIDER. If it does not, the provider re-authenticates silently from its own cookie
    and the person is waved through without typing anything -- it looks like a fresh login and is
    not one. Asserted on the URL we send the browser to, because the forwarding is our property and
    the provider's response is not."""
    first, _ = apps
    browser = Browser()

    started = first.authorize(browser, prompt="login", max_age="120")
    assert started.needs_login, started
    fed = first.start_federation(browser, UPSTREAM_PROVIDER)
    assert fed.status_code == 302, f"{fed.status_code} {fed.text[:200]}"
    upstream = fed.headers["Location"]
    assert "prompt=login" in upstream, \
        f"prompt=login did not reach the provider, so it means only 'skip our session': {upstream}"
    assert "max_age=120" in upstream, f"max_age did not reach the provider: {upstream}"


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


def test_sso_the_shortcut_still_honours_the_registered_redirect_uri(apps, espuser_person):
    """The SSO path issues a code without a login page, so it is the path where a skipped
    redirect_uri check would go unnoticed. An unregistered URI must be refused on our own page,
    before any code exists -- never by redirecting to it."""
    first, second = apps
    browser = Browser()
    assert first.sign_in(browser, espuser_person).used_upstream

    rogue = WebClient(second.client_id, "https://attacker.itest.example/callback",
                      scope=f"openid email {SESSIONS_SCOPE}")
    refused = rogue.authorize(browser)
    assert refused.code is None, "a live session must not excuse an unregistered redirect_uri"
    assert refused.response.status_code == 400, \
        f"an unregistered redirect_uri must fail on our page, never by redirecting to it: {refused!r}"


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


def test_sso_sessions_endpoint_requires_the_sessions_scope(register_espuser_client, apps, espuser_person):
    """Being able to sign somebody in is not a reason to enumerate every device they use, so the
    device list has a scope of its own -- and it must reject anything that is not an access token
    bearing it. A token without account:sessions is 403 with a challenge that names the scope
    (RFC 6750 s3); a missing token is 401 with a Bearer challenge; an id token -- validly signed by
    this same issuer but audienced to the client, not this API -- must be 401 too, or every client's
    own token becomes a key here (RFC 9700 token substitution)."""
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

    first, _ = apps
    tokens = signed_in(first, Browser(), espuser_person)
    missing = requests.get(f"{USER_API_GATEWAY_URL}/v1/user/sessions", timeout=30)
    assert missing.status_code == 401 and "Bearer" in bearer_challenge(missing), \
        f"a 401 with no challenge tells a client nothing about how to authenticate: {dict(missing.headers)}"
    substituted = WebClient.sessions(tokens["id_token"])
    assert substituted.status_code == 401, \
        "an id token is audienced to the client, not this API; accepting one here would make " \
        f"every client's own token a key to this endpoint: {substituted.text[:200]}"


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
