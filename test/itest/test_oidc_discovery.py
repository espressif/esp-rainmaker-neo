# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Integration tests for the public OIDC/OAuth discovery documents and the pages a browser is sent
to.

These are things only a deployment can answer. The discovery documents are static S3 objects written
by a lambda at deploy time, so what the code builds and what clients read are two different questions:
a unit test asserts the value in the struct, only a request shows what is being served. The security
headers are here for the same reason -- a handler setting a Content-Security-Policy proves nothing
about what ships, because API Gateway, CloudFront and the Lambda response mapping can each drop a
header on the way out, and a stripped CSP is invisible until somebody uses it.

Not here, because the unit specs already own them: the CSP's exact contents, the nonce being fresh
per render, display names being escaped, and the documents' cache lifetimes.

Spec: espuser/docs/en/specs/oidc-aouth2.md, sso-sessions.md.
"""
import pytest
import requests

from test.itest.conftest import SESSIONS_SCOPE, USER_API_GATEWAY_URL, esp_user_base_outputs

ISSUER = (esp_user_base_outputs.get('EspUserDiscoveryIssuer') or '').rstrip('/')

pytestmark = [
    pytest.mark.espuser,
    pytest.mark.skipif(not ISSUER, reason="EspUserDiscoveryIssuer output not present; redeploy espuser-base"),
]

OIDC_DOC = "/.well-known/openid-configuration"
OAUTH_DOC = "/.well-known/oauth-authorization-server"

STALE_DOC_HINT = (
    "The documents are static S3 objects written by the publish_discovery lambda at deploy time. If "
    "this fails after a code change, the stack needs redeploying (and the lambda's PublishVersion "
    "bumped) -- the value in the Go source is not what clients read."
)


def _get(path):
    return requests.get(f"{ISSUER}{path}", timeout=15)


def test_openid_configuration():
    """The openid-configuration document, and everything a client must be able to discover from it:
    the core OIDC metadata, plus the capabilities this deployment adds -- the sign-out endpoint (and
    that it is a route that actually exists, not a string that 404s), the account:sessions scope, every
    implemented grant, and resource-indicator support. A capability a client cannot discover is one no
    client will use."""
    resp = _get(OIDC_DOC)
    assert resp.status_code == 200, resp.text
    doc = resp.json()

    for field in (
        "issuer", "authorization_endpoint", "token_endpoint", "jwks_uri",
        "response_types_supported", "subject_types_supported",
        "id_token_signing_alg_values_supported",
    ):
        assert field in doc, f"missing required field {field}"
    assert doc["issuer"] == ISSUER
    assert doc["jwks_uri"] == f"{ISSUER}/.well-known/jwks.json"
    assert doc["response_types_supported"] == ["code"]
    assert doc["id_token_signing_alg_values_supported"] == ["RS256"]

    # A sign-out endpoint a client cannot discover is a sign-out button nobody can build -- and an
    # advertised URL that reaches API Gateway's "Missing Authentication Token" is a document that lies.
    end_session = doc.get("end_session_endpoint")
    assert end_session and end_session.endswith("/oauth2/logout"), \
        f"{OIDC_DOC} publishes no usable end_session_endpoint ({end_session!r}). {STALE_DOC_HINT}"
    routed = requests.get(end_session, allow_redirects=False, timeout=15)
    assert routed.status_code in (200, 302), f"{end_session} answered {routed.status_code}: {routed.text[:200]}"
    assert "Missing Authentication Token" not in routed.text, \
        f"{end_session} is advertised but not wired to the sessions lambda"

    # The sessions API refuses every token that did not ask for this scope, so a client that cannot
    # discover it will never ask.
    assert SESSIONS_SCOPE in doc.get("scopes_supported", []), \
        f"{OIDC_DOC} advertises {doc.get('scopes_supported')}. {STALE_DOC_HINT}"

    # Every grant the server implements must be advertised, client_credentials included -- the
    # assertion the old test_client_credentials.py used to make about this document.
    grants = doc.get("grant_types_supported", [])
    for grant in ("authorization_code", "refresh_token", "client_credentials"):
        assert grant in grants, f"{OIDC_DOC} advertises {grants}. {STALE_DOC_HINT}"

    # RFC 8707: how a client knows it may ask for an audience-scoped token instead of one that opens
    # every door.
    assert doc.get("resource_indicators_supported") is True, \
        f"{OIDC_DOC} does not advertise resource indicators. {STALE_DOC_HINT}"


def test_oauth_authorization_server():
    resp = _get(OAUTH_DOC)
    assert resp.status_code == 200, resp.text
    doc = resp.json()
    for field in ("issuer", "authorization_endpoint", "token_endpoint", "response_types_supported"):
        assert field in doc, f"missing required field {field}"
    assert doc["issuer"] == ISSUER
    assert doc["response_types_supported"] == ["code"]


def test_the_two_documents_agree():
    """A client that reads openid-configuration and a client that reads oauth-authorization-server
    must be looking at the same authorization server. They are built from the same lists, so a
    disagreement on any advertised capability means one was published from an older build -- and it
    is why the espuser-specific assertions above need only be made against one of the two."""
    oidc, oauth = _get(OIDC_DOC).json(), _get(OAUTH_DOC).json()
    for field in ("issuer", "authorization_endpoint", "token_endpoint", "revocation_endpoint",
                  "end_session_endpoint", "jwks_uri", "grant_types_supported",
                  "scopes_supported", "response_types_supported",
                  "code_challenge_methods_supported", "resource_indicators_supported"):
        assert oidc.get(field) == oauth.get(field), (
            f"{field}: openid-configuration says {oidc.get(field)!r}, "
            f"oauth-authorization-server says {oauth.get(field)!r}. {STALE_DOC_HINT}"
        )


def test_jwks():
    resp = _get('/.well-known/jwks.json')
    assert resp.status_code == 200, resp.text
    doc = resp.json()
    assert "keys" in doc and len(doc["keys"]) >= 1
    key = doc["keys"][0]
    for member in ("kty", "kid", "use", "alg", "n", "e"):
        assert member in key, f"missing required JWK member {member}"
    assert key["kty"] == "RSA"
    assert key["use"] == "sig"
    assert key["alg"] == "RS256"


# ── the headers a browser is actually served ──────────────────────────────────────
# The login page is the one page of ours a person types a credential into, and the only place in this
# system where a browser executes our markup. These assert what survives the gateway, never what the
# policy says -- the unit specs own the contents.


@pytest.mark.skipif(not USER_API_GATEWAY_URL, reason="EspUserApiUrl not configured")
def test_the_login_page_keeps_its_security_headers_through_the_gateway():
    resp = requests.get(f"{USER_API_GATEWAY_URL}/oauth2/login", timeout=15)
    assert resp.status_code == 200, f"{resp.status_code} {resp.text[:200]}"
    headers = {k.lower(): v for k, v in resp.headers.items()}

    csp = headers.get("content-security-policy", "")
    assert csp, "no Content-Security-Policy reached the browser; a stripped CSP is invisible until it matters"
    assert "frame-ancestors 'none'" in csp, f"clickjacking defence missing from the served CSP: {csp}"
    assert "default-src 'none'" in csp, f"the served CSP is not the deny-by-default one: {csp}"

    assert headers.get("x-frame-options") == "DENY", headers.get("x-frame-options")
    assert headers.get("x-content-type-options") == "nosniff", headers.get("x-content-type-options")
    assert headers.get("referrer-policy") == "no-referrer", (
        "the login URL carries the flow id and the authorize parameters; a Referer to any resource on "
        f"the page would leak them: {headers.get('referrer-policy')}"
    )
    assert "no-store" in headers.get("cache-control", ""), (
        "a cached login page is a login page served to the next person on a shared machine: "
        f"{headers.get('cache-control')}"
    )


@pytest.mark.skipif(not USER_API_GATEWAY_URL, reason="EspUserApiUrl not configured")
def test_the_error_page_keeps_them_too():
    """The error page renders caller-supplied context, so it is the likelier injection target of the
    two -- and the easier one to forget when headers are added."""
    resp = requests.get(f"{USER_API_GATEWAY_URL}/oauth2/authorize",
                        params={"response_type": "token"}, allow_redirects=False, timeout=15)
    assert resp.status_code == 400, f"{resp.status_code} {resp.text[:200]}"
    headers = {k.lower(): v for k, v in resp.headers.items()}
    assert "frame-ancestors 'none'" in headers.get("content-security-policy", "")
    assert headers.get("x-frame-options") == "DENY"
    assert headers.get("x-content-type-options") == "nosniff"
    assert headers.get("referrer-policy") == "no-referrer"
