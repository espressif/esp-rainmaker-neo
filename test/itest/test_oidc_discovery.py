# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Integration tests for the public OIDC/OAuth discovery documents.

These are static, unauthenticated S3 objects served at <issuer>/.well-known/*.
Spec: espuser/docs/en/specs/oidc-aouth2.md.
"""
import requests
import pytest

from test.itest.conftest import esp_user_base_outputs

ISSUER = (esp_user_base_outputs.get('EspUserDiscoveryIssuer') or '').rstrip('/')

pytestmark = pytest.mark.skipif(
    not ISSUER,
    reason="EspUserDiscoveryIssuer output not present; redeploy espuser-base",
)


def _get(path):
    return requests.get(f"{ISSUER}{path}", timeout=10)


def test_openid_configuration():
    resp = _get('/.well-known/openid-configuration')
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


def test_oauth_authorization_server():
    resp = _get('/.well-known/oauth-authorization-server')
    assert resp.status_code == 200, resp.text
    doc = resp.json()
    for field in ("issuer", "authorization_endpoint", "token_endpoint", "response_types_supported"):
        assert field in doc, f"missing required field {field}"
    assert doc["issuer"] == ISSUER
    assert doc["response_types_supported"] == ["code"]


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


def test_discovery_is_cacheable():
    resp = _get('/.well-known/openid-configuration')
    assert resp.status_code == 200
    assert "max-age" in resp.headers.get("Cache-Control", "")
