# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""A browser and a web client, so a test can be about more than one of each.

Everything else in the suite drives one login for one client and throws the cookie jar
away. That is enough to test a rule and useless for testing a *session*: single sign-on,
two devices, "sign the laptop out from the phone" and "one sign-out reaches every product"
are all statements about several requests sharing (or not sharing) one browser.

Two ideas, and they are the whole module:

  Browser -- a requests.Session, which is to say a cookie jar. Two Browsers are two
      devices. The session cookie is HttpOnly and __Host- prefixed, so this is also the
      only way to observe it: whatever the jar holds is exactly what a real browser would.

  WebClient -- one simulated product, with its own client_id and redirect_uri and no
      browser of its own. The same client can be driven from two Browsers, and three
      clients can be driven from one; that combination is what makes the sign-out tests
      possible.

`pkce_pair` and `cognito_hosted_login` live here, and the API base URL is injected by the
harness (`API_BASE`, set by conftest from the deployment outputs), so this is a py_sdk client
that imports nothing from the test dir. `upstream_login` below is the only function here that
knows which identity provider is upstream; a deployment federating to something other than
Cognito replaces that one function and nothing else.
"""
import os
from urllib.parse import parse_qs, urlparse

import requests

#: Cookie names -- protocol constants (the __Host- prefix is a security property), not
#: deployment config, so the client library that speaks to these endpoints owns them.
SESSION_COOKIE = "__Host-esp_session"
FLOW_COOKIE = "__Host-esp_flow_id"
#: The upstream identity provider a login federates to; overridable per deployment.
UPSTREAM_PROVIDER = os.getenv("ESPUSER_ITEST_PROVIDER", "cognito")

#: The ESP-User API base URL. Injected by the harness (conftest sets it from the deployment
#: outputs) rather than imported, so this module is a py_sdk client with no dependency on the
#: test dir. Set once, before any test runs.
API_BASE = ""


def pkce_pair():
    """Return (verifier, S256 challenge) for a PKCE code exchange (RFC 7636)."""
    import base64
    import hashlib
    import secrets
    verifier = base64.urlsafe_b64encode(secrets.token_bytes(32)).rstrip(b"=").decode()
    challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()
    return verifier, challenge


def cognito_hosted_login(session, hosted_authorize_url, email, password):
    """Script the Cognito hosted-UI password login: follow to the login form, POST the
    credentials with the CSRF token, and return the redirect back to our federation callback."""
    page = session.get(hosted_authorize_url, allow_redirects=True)
    assert page.status_code == 200, f"hosted UI login page failed: {page.status_code}"
    csrf = session.cookies.get("XSRF-TOKEN")
    assert csrf, "hosted UI did not set the XSRF-TOKEN cookie"
    login = session.post(page.url, data={"_csrf": csrf, "username": email, "password": password},
                         allow_redirects=False)
    assert login.status_code == 302, \
        f"hosted UI login failed for {email}: {login.status_code} {login.text[:300]}"
    return login.headers["Location"]


DEFAULT_SCOPE = "openid email account:sessions"

#: Where the upstream returns the browser to us. Used to tell "the provider is showing a
#: login form" apart from "the provider answered without one".
FEDERATION_CALLBACK_PATH = "/oauth2/federation/callback"


def sign_out_destination(browser, response, limit=6):
    """Where a sign-out actually leaves the person, following the whole chain.

    Sign-out is a chain, not one redirect. When the deployment configures an upstream
    end-session endpoint the browser goes to the provider first, comes back to
    /oauth2/logout/done, and only then reaches the product's own page; with no upstream
    configured it goes straight there. Asserting the FIRST Location pins whichever shape a
    deployment happens to have, and would fail the day an operator configures the other one.

    The landing page belongs to the product and generally does not exist in a test account, so
    a 404 at the end is expected and irrelevant -- the URL is the answer, not the status.
    """
    url = response.headers.get("Location", "")
    for _ in range(limit):
        if not url:
            return ""
        try:
            hop = browser.session.get(url, allow_redirects=False, timeout=30)
        except requests.RequestException:
            return url  # cannot reach it; the URL we were sent to is still the answer
        nxt = hop.headers.get("Location", "")
        if not nxt:
            return url
        url = nxt
    return url


def upstream_login(session, hosted_url, username, password):
    """Get the browser through the upstream leg and return the callback URL it lands on.

    Usually that means filling in the provider's hosted password form. But a provider that
    still holds a session of its own may answer the authorize request outright, with no form
    to fill -- and that is legitimate: ending our session ends OUR session, and a provider's
    cookie is its own business. It is also what happens on any second login in one browser,
    since the provider's cookie survives our sign-out unless the provider row configures an
    end_session_url.

    So the redirect is inspected before assuming there is a form. Without this, a silent
    upstream re-authentication surfaces as a 404 from whatever host the authorization code
    was finally delivered to, several frames away from the cause.

    The only provider-specific step in this module. Everything above it is protocol.
    """
    probe = session.get(hosted_url, allow_redirects=False)
    if probe.is_redirect:
        location = probe.headers.get("Location", "")
        if FEDERATION_CALLBACK_PATH in location:
            return location
    return cognito_hosted_login(session, hosted_url, username, password)


class Browser:
    """One browser: one cookie jar, one device row in the sessions list."""

    def __init__(self, user_agent="espuser-itest/1.0"):
        self.session = requests.Session()
        self.session.headers["User-Agent"] = user_agent
        #: The raw Set-Cookie header last seen carrying the session cookie. Kept because the
        #: attributes only exist on the wire -- a cookie jar has already dropped them, and
        #: whether the browser would ACCEPT the cookie is decided by exactly those bytes.
        self.last_session_set_cookie = None

    @property
    def session_cookie(self):
        """The session cookie value this browser holds, or None."""
        return self.session.cookies.get(SESSION_COOKIE)

    @property
    def flow_cookie(self):
        return self.session.cookies.get(FLOW_COOKIE)

    def plant_session_cookie(self, value):
        """Put a chosen value in the jar, as a fixation attacker would wish to. The server
        must never adopt it."""
        host = urlparse(API_BASE).hostname
        self.session.cookies.set(SESSION_COOKIE, value, domain=host, path="/", secure=True)

    def forget_session_cookie(self):
        """Drop the session cookie without telling the server -- a closed browser, not a
        sign-out."""
        self.session.cookies.pop(SESSION_COOKIE, None)

    def _remember_session_cookie(self, response):
        header = response.headers.get("Set-Cookie", "")
        if SESSION_COOKIE in header:
            self.last_session_set_cookie = header


class Authorization:
    """What /oauth2/authorize answered: a code, an error, or "you must log in"."""

    def __init__(self, client, response, verifier, state):
        self.client, self.response, self.verifier, self.state = client, response, verifier, state
        self.location = response.headers.get("Location", "")
        self._query = parse_qs(urlparse(self.location).query) if self.location else {}

    @property
    def returned_to_client(self):
        """True when the server answered the client directly rather than showing a page."""
        return self.location.startswith(self.client.redirect_uri)

    @property
    def code(self):
        return self._query.get("code", [None])[0] if self.returned_to_client else None

    @property
    def error(self):
        return self._query.get("error", [None])[0] if self.returned_to_client else None

    @property
    def returned_state(self):
        return self._query.get("state", [None])[0] if self.returned_to_client else None

    @property
    def needs_login(self):
        """True when the browser was sent to a login page or straight at the provider."""
        return self.response.status_code == 302 and not self.returned_to_client

    def __repr__(self):  # pragma: no cover -- assertion messages only
        return f"<Authorization {self.response.status_code} {self.location[:120]}>"


class WebClient:
    """One simulated product. Holds a client registration, never a browser."""

    def __init__(self, client_id, redirect_uri, *, scope=DEFAULT_SCOPE, resource=None,
                 client_secret=None, post_logout_redirect_uri=None):
        self.client_id = client_id
        self.redirect_uri = redirect_uri
        self.scope = scope
        self.resource = resource
        self.client_secret = client_secret
        self.post_logout_redirect_uri = post_logout_redirect_uri

    # ── the authorization leg ─────────────────────────────────────────────────────

    def authorize(self, browser, *, state="itest-state", **extra):
        """GET /oauth2/authorize. Never follows the redirect: whether it is a code, an error
        or a login page IS the result."""
        verifier, challenge = pkce_pair()
        params = {
            "response_type": "code",
            "client_id": self.client_id,
            "redirect_uri": self.redirect_uri,
            "scope": self.scope,
            "state": state,
            "code_challenge": challenge,
            "code_challenge_method": "S256",
        }
        if self.resource:
            params["resource"] = self.resource
        params.update({k: v for k, v in extra.items() if v is not None})
        resp = browser.session.get(f"{API_BASE}/oauth2/authorize", params=params,
                                   allow_redirects=False)
        browser._remember_session_cookie(resp)
        return Authorization(self, resp, verifier, state)

    def start_federation(self, browser, provider=UPSTREAM_PROVIDER):
        """GET /oauth2/federation/start -- the hop that hands the browser to the provider.
        Returned unfollowed so a test can read what we asked the provider for."""
        return browser.session.get(f"{API_BASE}/oauth2/federation/start",
                                   params={"provider": provider}, allow_redirects=False)

    def sign_in(self, browser, person, *, provider=UPSTREAM_PROVIDER, **extra):
        """The whole browser login, ending at a code.

        Returns the Authorization. When a live session already answers, no upstream round
        trip happens at all -- which is single sign-on, and `used_upstream` says which
        happened.
        """
        authz = self.authorize(browser, **extra)
        if authz.code:
            authz.used_upstream = False
            return authz
        assert authz.needs_login, f"expected a login page or a code, got {authz!r}"

        fed = self.start_federation(browser, provider)
        assert fed.status_code == 302, f"federation start: {fed.status_code} {fed.text[:200]}"
        callback_url = upstream_login(browser.session, fed.headers["Location"],
                                      person.email, person.password)
        cb = browser.session.get(callback_url, allow_redirects=False)
        browser._remember_session_cookie(cb)
        assert cb.status_code == 302, f"federation callback: {cb.status_code} {cb.text[:300]}"

        done = Authorization(self, cb, authz.verifier, authz.state)
        done.used_upstream = True
        return done

    # ── the token leg ─────────────────────────────────────────────────────────────

    def exchange(self, authz):
        """Redeem an authorization code. Returns the raw response so negatives can read it."""
        return self.exchange_code(authz.code, authz.verifier)

    def exchange_code(self, code, verifier, *, client_id=None, redirect_uri=None):
        data = {
            "grant_type": "authorization_code",
            "code": code,
            "code_verifier": verifier,
            "client_id": client_id or self.client_id,
            "redirect_uri": redirect_uri or self.redirect_uri,
        }
        if self.client_secret:
            data["client_secret"] = self.client_secret
        return requests.post(f"{API_BASE}/oauth2/token", data=data, timeout=30)

    def tokens(self, authz):
        """The token set, asserting the exchange worked. The common case."""
        resp = self.exchange(authz)
        assert resp.status_code == 200, f"token exchange: {resp.status_code} {resp.text[:300]}"
        return resp.json()

    def refresh(self, refresh_token):
        return requests.post(f"{API_BASE}/oauth2/token", data={
            "grant_type": "refresh_token",
            "refresh_token": refresh_token,
            "client_id": self.client_id,
        }, timeout=30)

    def still_signed_in(self, refresh_token):
        """Whether this product can still get a new access token. The only honest test of
        "am I signed out" -- an access token already issued keeps working until it expires."""
        return self.refresh(refresh_token).status_code == 200

    # ── the session-management leg ────────────────────────────────────────────────

    @staticmethod
    def sessions(access_token):
        return requests.get(f"{API_BASE}/v1/user/sessions",
                            headers={"Authorization": f"Bearer {access_token}"}, timeout=30)

    @staticmethod
    def end_session(access_token, sid):
        return requests.delete(f"{API_BASE}/v1/user/sessions/{sid}",
                               headers={"Authorization": f"Bearer {access_token}"}, timeout=30)

    @staticmethod
    def end_all_sessions(access_token):
        return requests.delete(f"{API_BASE}/v1/user/sessions",
                               headers={"Authorization": f"Bearer {access_token}"}, timeout=30)

    def logout(self, browser, *, id_token_hint=None, post_logout_redirect_uri=None,
               with_client_id=True):
        """GET /oauth2/logout as a top-level navigation, unfollowed.

        Unfollowed on purpose: the Set-Cookie that clears the session and the Location that
        sends the browser onward are both part of what sign-out means, and following the
        redirect would discard them.
        """
        params = {}
        if with_client_id:
            params["client_id"] = self.client_id
        if id_token_hint:
            params["id_token_hint"] = id_token_hint
        target = post_logout_redirect_uri or self.post_logout_redirect_uri
        if target:
            params["post_logout_redirect_uri"] = target
        resp = browser.session.get(f"{API_BASE}/oauth2/logout", params=params,
                                   allow_redirects=False)
        browser._remember_session_cookie(resp)
        return resp


def sids_in(listing):
    """Every session id in a /v1/user/sessions body."""
    return [s["session_id"] for s in listing.get("sessions", [])]


def session_with(listing, sid):
    for s in listing.get("sessions", []):
        if s["session_id"] == sid:
            return s
    return None


def client_ids_under(session_view):
    return sorted(p["client_id"] for p in (session_view or {}).get("clients", []))
