# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import base64
import json
import time
import uuid

import boto3
import pytest
import requests

from py_sdk.espuser_oauth import (
    Browser,
    WebClient,
    client_ids_under,
    session_with,
    sids_in,
    sign_out_destination,
)
from py_sdk.test_user import User
from test.itest.conftest import (
    ADMIN_CLIENT_ID,
    ADMIN_USER_POOL_ID,
    API_GATEWAY_URL,
    END_USER_POOL_ID,
    ESP_USER_CLIENT_ID,
    ESPUSER_RESOURCE,
    IDENTITY_POOL_ID,
    IOT_ENDPOINT,
    NEO_RESOURCE,
    REGION,
    USER_API_GATEWAY_URL,
    complete_federation_login,
    decode_jwt_claims,
    requires_espuser,
)
from test.itest.email_utils import (
    generate_random_email,
    generate_test_password,
)


def _new_user(email):
    """A bare, unauthenticated User helper bound to an email (no signin)."""
    user = User(email, "unused", REGION, IDENTITY_POOL_ID,
                API_GATEWAY_URL, USER_API_GATEWAY_URL, IOT_ENDPOINT)
    user.mailosaur_email = email
    return user


def _second_login(user):
    """The same person as `user` on a User of its own, so a test that rotates or revokes its login never spends the pooled user's refresh token, which user_api_token() redeems for whichever test borrows it next."""
    return User(user.username, user.password, REGION, IDENTITY_POOL_ID,
                API_GATEWAY_URL, USER_API_GATEWAY_URL, IOT_ENDPOINT)


def _assert_neo_takes_only_its_own_token(neo_tokens, espuser_token):
    """neo accepts only its own token and refuses the ESP User API's with 401 (RFC 9068 s4, RFC 6750 s3.1); the ESP User API's own endpoints take a token for either."""
    neo_token, id_token = neo_tokens["access_token"], neo_tokens["id_token"]
    creds_url = f"{API_GATEWAY_URL}/v1/user/credentials"
    me_url = f"{USER_API_GATEWAY_URL}/v1/users/me"
    bearer = lambda token: {"Authorization": f"Bearer {token}"}  # noqa: E731
    assert requests.post(creds_url, headers=bearer(neo_token), json={"id_token": id_token}).status_code == 200
    assert requests.post(creds_url, headers=bearer(espuser_token), json={"id_token": id_token}).status_code == 401, \
        "neo must refuse a token addressed to the ESP User API"
    assert requests.get(me_url, headers=bearer(espuser_token)).status_code == 200
    assert requests.get(me_url, headers=bearer(neo_token)).status_code == 200, \
        "the ESP User API's own endpoints take a token addressed to any of our APIs"




def test_signin_returns_tokens(test_user1):
    """The password signin yields the full token set — our tokens, never the provider's — and
    populates the instance tokens."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    response = test_user1.signin()
    assert response.status_code == 200, f"Expected 200, got {response.status_code}: {response.text}"
    data = response.json()
    assert "access_token" in data
    assert "refresh_token" in data
    assert "id_token" in data
    assert test_user1.access_token is not None
    assert test_user1.refresh_token is not None


def test_an_old_app_build_signs_in_renews_and_reaches_its_devices_without_naming_a_client(test_user1):
    """The store app sends the password API only a username and password and renews through its refresh endpoint; its sign-in belongs to EspUserClientId, the Home app client, which /v1/user/credentials and the identity pool trust."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")
    assert ESP_USER_CLIENT_ID, "espuser-base must publish EspUserClientId"
    auth_url = f"{USER_API_GATEWAY_URL}/v1/user/auth"
    bearer = lambda token: {"Authorization": f"Bearer {token}"}  # noqa: E731

    signin = requests.post(f"{auth_url}/token", json={"username": test_user1.username, "password": test_user1.password})
    assert signin.status_code == 200, signin.text
    tokens = signin.json()
    aud = decode_jwt_claims(tokens["id_token"])["aud"]
    assert (aud if isinstance(aud, list) else [aud]) == [ESP_USER_CLIENT_ID], f"the id token must be for the Home app client: {aud}"
    assert decode_jwt_claims(tokens["access_token"])["client_id"] == ESP_USER_CLIENT_ID

    renewed = requests.post(f"{auth_url}/token/refresh", json={"refresh_token": tokens["refresh_token"]})
    assert renewed.status_code == 200, renewed.text
    renewed_tokens = renewed.json()
    assert renewed_tokens["refresh_token"] != tokens["refresh_token"], "every renewal must rotate"
    assert decode_jwt_claims(renewed_tokens["access_token"])["client_id"] == ESP_USER_CLIENT_ID

    creds = requests.post(f"{API_GATEWAY_URL}/v1/user/credentials", headers=bearer(renewed_tokens["access_token"]),
                          json={"id_token": renewed_tokens["id_token"]})
    assert creds.status_code == 200, f"the renewed pair must get AWS credentials for the devices: {creds.text}"
    assert creds.json().get("access_key_id"), creds.text

    me = requests.get(f"{USER_API_GATEWAY_URL}/v1/users/me", headers=bearer(renewed_tokens["access_token"]))
    assert me.status_code == 200, f"the profile must answer the renewed token: {me.text}"


# Signup enumeration resistance moved to test_cognito_auth_matrix.py, which asserts it
# against the provisioned pool and an external one.


# --- Admin (Cognito) and end user (OIDC) with the same email are separate identities. ---


@pytest.mark.xdist_group("env_mut")
def test_same_email_as_admin_and_end_user_are_separate_identities():
    """One person may hold both roles. The credentials live in different pools and are validated
    against different issuers, so the two logins resolve to different subjects and neither can be
    mistaken for the other."""
    if not USER_API_GATEWAY_URL or not END_USER_POOL_ID or not ADMIN_USER_POOL_ID:
        pytest.skip("espuser outputs not configured")

    email = generate_random_email() or f"dual-{uuid.uuid4().hex[:10]}@example.com"
    password = generate_test_password()
    cognito = boto3.client('cognito-idp', region_name=REGION)

    admin = User(email, password, REGION, IDENTITY_POOL_ID, API_GATEWAY_URL, USER_API_GATEWAY_URL,
                 IOT_ENDPOINT, admin_user_pool_id=ADMIN_USER_POOL_ID, admin_client_id=ADMIN_CLIENT_ID,
                 is_admin=True)
    end_user = User(email, password, REGION, IDENTITY_POOL_ID, API_GATEWAY_URL, USER_API_GATEWAY_URL,
                    IOT_ENDPOINT)
    end_user.end_user_pool_id = END_USER_POOL_ID
    try:
        assert admin.create_admin_via_cognito(email=email, password=password), \
            "the same email must be usable in the admin pool"
        assert end_user.create_user_via_cognito(email=email, password=password), \
            "and independently in the end-user pool"

        admin_signin = admin.signin(is_admin=True)
        assert admin_signin.status_code == 200, f"admin signin: {admin_signin.status_code} {admin_signin.text}"
        admin_claims = decode_jwt_claims(admin.token)
        assert "cognito-idp" in admin_claims["iss"], \
            f"an admin presents a provider token: {admin_claims['iss']}"

        # The password surface, not federation: this is about which issuer signs each role's token,
        # and the plain signin exercises that with one call.
        user_signin = end_user.signin()
        assert user_signin.status_code == 200, f"end-user signin: {user_signin.status_code} {user_signin.text}"
        user_claims = decode_jwt_claims(user_signin.json()["access_token"])
        assert "cognito-idp" not in user_claims["iss"], \
            f"an end user presents our own token: {user_claims['iss']}"
        assert user_claims["email"] == email

        admin_subject = admin_claims.get("custom:user_id") or admin_claims["sub"]
        assert user_claims["sub"] != admin_subject, \
            "the two roles must be distinct subjects even though they share an email"
    finally:
        for pool in (ADMIN_USER_POOL_ID, END_USER_POOL_ID):
            try:
                cognito.admin_delete_user(UserPoolId=pool, Username=email)
            except Exception:  # noqa: BLE001
                pass
        try:
            end_user.delete_otp_user_by_email(email)
        except Exception:  # noqa: BLE001
            pass



# --- Core OAuth semantics. These hold for any login method, so they use the cheapest one. ---


# Refresh rotation and reuse detection moved to test_cognito_auth_matrix.py, which asserts
# them against the provisioned pool and an external one.


def test_refresh_grace_tolerates_lost_response_retry(test_user1):
    """A dropped rotation response must not unlink the client. Re-presenting the just-spent token
    within the grace window re-issues the rotated token idempotently instead of killing the family
    as reuse — otherwise ordinary network loss would permanently unlink voice assistants.
    """
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    signin = test_user1.signin()
    assert signin.status_code == 200, signin.text
    original = signin.json()["refresh_token"]

    first = test_user1.refresh_tokens(original)
    assert first.status_code == 200, first.text
    rotated = first.json()["refresh_token"]
    assert rotated != original, "the first redemption must rotate"

    # The client never received `first`'s response and retries with the only token it still holds.
    retry = test_user1.refresh_tokens(original)
    assert retry.status_code == 200, f"grace retry must succeed: {retry.status_code} {retry.text}"
    assert retry.json()["refresh_token"] == rotated, "grace must re-issue the same rotated token"

    # The family survived, so the rotated token still advances normally.
    assert test_user1.refresh_tokens(rotated).status_code == 200


def test_refresh_rejects_unknown_token():
    """A token that was never issued is refused with invalid_grant, giving no reuse oracle."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    resp = _new_user("nobody@example.com").refresh_tokens(refresh_token="nope.nope")
    assert resp.status_code == 400, resp.text
    assert resp.json().get("error") == "invalid_grant", resp.text


def test_userinfo_returns_scope_gated_claims_and_refuses_bad_tokens(test_user1):
    """/oauth2/userinfo resolves an access token to its subject's scope-gated claims, and refuses
    anything that is not one."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    signin = test_user1.signin()
    assert signin.status_code == 200, signin.text

    info = test_user1.oauth_userinfo(signin.json()["access_token"])
    assert info.status_code == 200, info.text
    claims = info.json()
    assert claims.get("sub"), f"sub is always present: {claims}"
    assert claims.get("email") == test_user1.username.lower(), \
        f"email scope was granted, so email must be returned: {claims}"

    rejected = test_user1.oauth_userinfo("not.a.jwt")
    assert rejected.status_code == 401, rejected.text
    assert rejected.json().get("error") == "invalid_token", rejected.text


def test_userinfo_accepts_a_token_for_any_of_our_apis(test_user1):
    """/oauth2/userinfo is the issuer's own endpoint (OIDC Core s5.3), not one API's, so one sign-in's neo token and ESP User API token both resolve there to the same subject."""
    signin = test_user1.signin()
    assert signin.status_code == 200, signin.text
    neo_token = signin.json()["access_token"]
    espuser_token = test_user1.user_api_token()
    assert decode_jwt_claims(neo_token)["aud"] == NEO_RESOURCE
    assert decode_jwt_claims(espuser_token)["aud"] == ESPUSER_RESOURCE

    subjects = []
    for token in (neo_token, espuser_token):
        info = test_user1.oauth_userinfo(token)
        assert info.status_code == 200, \
            f"userinfo must accept a token for {decode_jwt_claims(token)['aud']}: {info.status_code} {info.text}"
        subjects.append(info.json().get("sub"))
    assert subjects[0] == subjects[1] == decode_jwt_claims(neo_token)["sub"], f"one sign-in is one subject: {subjects}"


def test_revocation_ends_the_login_on_both_surfaces(test_user1):
    """Revoking ends a login wherever it is asked: the RFC 7009 endpoint and the legacy signout both
    leave the presented refresh token unredeemable."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    login = _second_login(test_user1)
    first = login.signin()
    assert first.status_code == 200, first.text
    token = first.json()["refresh_token"]
    assert login.oauth_revoke(token, token_type_hint="refresh_token").status_code == 200
    assert login.refresh_tokens(token).status_code == 400, "a revoked token must not redeem"

    # A fresh login, ended through the legacy surface instead.
    second = login.signin()
    assert second.status_code == 200, second.text
    legacy_token = second.json()["refresh_token"]
    assert login.signout(refresh_token=legacy_token).status_code == 200
    assert login.refresh_tokens(legacy_token).status_code != 200, \
        "signout must revoke the family it was given"


def test_credentials_endpoint_rejects_id_token(test_user1):
    """RFC 9700 token substitution: the Authorization header of /v1/user/credentials must
    carry an ACCESS token. An id token is validly signed by the same issuer but is audienced
    to the client, so a request authenticated with one must be refused."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    signin = test_user1.signin()
    assert signin.status_code == 200, signin.text
    tokens = signin.json()
    id_token, access_token = tokens.get("id_token"), tokens.get("access_token")
    assert id_token and access_token, "a login must return both an id and an access token"

    creds_url = f"{API_GATEWAY_URL}/v1/user/credentials"
    rejected = requests.post(creds_url, headers={"Authorization": f"Bearer {id_token}"},
                             json={"id_token": id_token})
    assert rejected.status_code == 401, \
        f"an id token in the Authorization header must be rejected, got {rejected.status_code}: {rejected.text}"
    # The body is mandatory: the endpoint pairs the two halves of one sign-in.
    no_body = requests.post(creds_url, headers={"Authorization": f"Bearer {access_token}"})
    assert no_body.status_code == 400, \
        f"a request without the body id_token must be a 400, got {no_body.status_code}: {no_body.text}"
    # Positive control, so the rejections above cannot be passing for an unrelated reason.
    accepted = requests.post(creds_url, headers={"Authorization": f"Bearer {access_token}"},
                             json={"id_token": id_token})
    assert accepted.status_code == 200, \
        f"the matched pair from the same login must be accepted, got {accepted.status_code}: {accepted.text}"


def test_password_sign_in_covers_neo_and_esp_user_one_token_each(test_user1):
    """One password sign-in, which names no resource, covers both APIs the password client is granted
    (RFC 8707): neo accepts only its own token and refuses the other with 401 (RFC 9068 s4, RFC 6750
    s3.1), while the ESP User API takes either; the ESP User token comes from a refresh that names it."""
    signin = test_user1.signin()
    assert signin.status_code == 200, signin.text
    tokens = signin.json()
    assert decode_jwt_claims(tokens["access_token"])["aud"] == NEO_RESOURCE, "the password client's default"

    espuser_token = test_user1.user_api_token()
    assert decode_jwt_claims(espuser_token)["aud"] == ESPUSER_RESOURCE

    _assert_neo_takes_only_its_own_token(tokens, espuser_token)


def test_rotated_refresh_token_still_covers_every_api_of_the_sign_in(test_user1):
    """Rotation renews a sign-in, never narrows it (RFC 8707 s2.2): after two rotations the refresh token still gets neo a token and the ESP User API a token, and neo accepts only its own."""
    login = _second_login(test_user1)
    signin = login.signin()
    assert signin.status_code == 200, signin.text
    refresh_token = signin.json()["refresh_token"]
    for _ in range(2):
        rotated = login.refresh_tokens(refresh_token)
        assert rotated.status_code == 200, rotated.text
        assert rotated.json()["refresh_token"] != refresh_token, "every redemption must rotate"
        refresh_token = rotated.json()["refresh_token"]

    espuser = login.refresh_tokens(refresh_token, resource=ESPUSER_RESOURCE)
    assert espuser.status_code == 200, f"the rotated token must still cover the ESP User API: {espuser.text}"
    neo = login.refresh_tokens(espuser.json()["refresh_token"], resource=NEO_RESOURCE)
    assert neo.status_code == 200, f"and still cover neo after narrowing to the ESP User API: {neo.text}"
    espuser_token, neo_tokens = espuser.json()["access_token"], neo.json()
    assert decode_jwt_claims(espuser_token)["aud"] == ESPUSER_RESOURCE
    assert decode_jwt_claims(neo_tokens["access_token"])["aud"] == NEO_RESOURCE

    _assert_neo_takes_only_its_own_token(neo_tokens, espuser_token)


# --- Federated identity: which contact resolves an account, and which claims ride along. ---

_FED_REDIRECT_URI = "com.espressif.rainmaker://fed-itest"
_FED_SCOPE = "openid email phone"

# Standard OIDC profile attributes a federated login carries over.
_UPSTREAM_PROFILE = {
    "name": "Ada Lovelace",
    "locale": "en-GB",
    "picture": "https://img.example/ada.png",
}


@pytest.fixture
def fed_identity():
    """Distinct email/phone/password for one test, so reruns never collide."""
    tag = uuid.uuid4().hex[:10]
    # A +1555 number is reserved for testing and is never dialled.
    return {
        "email": f"fed-{tag}@example.com",
        "phone": "+1555" + tag[:7].translate(str.maketrans("abcdef", "012345")),
        "password": generate_test_password(),
    }


def _require_fed_env():
    if not USER_API_GATEWAY_URL or not END_USER_POOL_ID:
        pytest.skip("espuser outputs not configured")


@pytest.fixture
def fed_client(admin_user):
    """A throwaway OAuth client with our redirect_uri registered.

    The seeded first-party clients carry no redirect_uris, so /oauth2/authorize refuses them — a
    federated login needs a client that names where the code may be sent.
    """
    client_id = "itest_fed_" + uuid.uuid4().hex[:8]
    created = admin_user.create_oauth_client({
        "client_id": client_id, "client_name": "itest federation", "client_type": "public",
        "redirect_uris": [_FED_REDIRECT_URI], "grant_types": ["authorization_code", "refresh_token"],
        "scopes": ["openid", "email", "phone", "profile"], "require_pkce": True,
        "allowed_resources": [NEO_RESOURCE], "default_resource": NEO_RESOURCE,
    })
    assert created.status_code == 201, created.text
    yield client_id
    admin_user.delete_oauth_client(client_id)


def _login(client_id, username, password, scope=_FED_SCOPE, **kwargs):
    return complete_federation_login(client_id, _FED_REDIRECT_URI, username, password, scope, **kwargs)


@pytest.fixture
def federated_identity(fed_identity, provision_end_user):
    """One provider account carrying both verified contacts and the profile attributes, so a single
    login can be asserted from every angle instead of re-driving the flow per assertion."""
    email, phone, password = fed_identity["email"], fed_identity["phone"], fed_identity["password"]
    provision_end_user(email, dict({
        "email": email, "email_verified": "true",
        "phone_number": phone, "phone_number_verified": "true",
    }, **_UPSTREAM_PROFILE), password)
    return email, phone, password


@pytest.mark.xdist_group("env_mut")
def test_federated_login_carries_contacts_and_claims(federated_identity, fed_client, hosted_cognito_provider):
    """One brokered login, asserted end to end: both verified contacts are stored, the upstream
    profile reaches both tokens, userinfo agrees with them, a refresh preserves them and the
    original auth_time, and a second login reuses the account rather than creating another.

    Before the real callback, the same callback URL is opened from another browser, which must be
    refused before the flow is read (RFC 6749 s10.12), so the real callback still completes
    afterwards. Cognito stamps auth_time when its code is redeemed, so here auth_time is only
    checked as this sign-in; that it carries the upstream's value rather than our callback time is
    pinned by the authorize unit specs, whose fake upstream reports an older login."""
    _require_fed_env()
    email, phone, password = federated_identity

    def from_another_browser(callback_url):
        forged = requests.get(callback_url, allow_redirects=False)
        assert forged.status_code == 400, \
            f"a callback opened without the flow cookie must be refused: {forged.status_code} {forged.text[:200]}"

    started = time.time()
    tokens = _login(fed_client, email, password, "openid email phone profile",
                    provider=hosted_cognito_provider, before_callback=from_another_browser)

    id_claims = decode_jwt_claims(tokens["id_token"])
    assert abs(id_claims["auth_time"] - started) < 300 and id_claims["auth_time"] <= id_claims["iat"], \
        f"auth_time does not name this sign-in: {id_claims}"

    for label in ("access_token", "id_token"):
        claims = decode_jwt_claims(tokens[label])
        assert claims["email"] == email, f"{label}: {claims}"
        assert claims.get("phone_number") == phone, f"{label} lost the second contact: {claims}"
        for claim, expected in _UPSTREAM_PROFILE.items():
            assert claims.get(claim) == expected, \
                f"{label} missing upstream {claim}: got {claims.get(claim)!r}, want {expected!r}"

    info = requests.get(f"{USER_API_GATEWAY_URL}/oauth2/userinfo",
                        headers={"Authorization": f"Bearer {tokens['access_token']}"})
    assert info.status_code == 200, f"userinfo failed: {info.status_code} {info.text[:300]}"
    assert info.json()["email"] == email
    for claim, expected in _UPSTREAM_PROFILE.items():
        assert info.json().get(claim) == expected, f"userinfo missing {claim}: {info.json()}"

    # Claims come from our own store, not the upstream token, so a refresh must keep them.
    refreshed = requests.post(f"{USER_API_GATEWAY_URL}/oauth2/token", data={
        "grant_type": "refresh_token", "refresh_token": tokens["refresh_token"],
        "client_id": fed_client,
    })
    assert refreshed.status_code == 200, f"refresh failed: {refreshed.status_code} {refreshed.text[:300]}"
    assert "no-store" in refreshed.headers.get("Cache-Control", ""), \
        f"a token response must never be cached (RFC 6749 s5.1): {refreshed.headers.get('Cache-Control')}"
    refreshed_claims = decode_jwt_claims(refreshed.json()["access_token"])
    for claim, expected in _UPSTREAM_PROFILE.items():
        assert refreshed_claims.get(claim) == expected, f"refreshed token lost {claim}: {refreshed_claims}"
    assert decode_jwt_claims(refreshed.json()["id_token"])["auth_time"] == id_claims["auth_time"], \
        "a refresh is not an authentication: its ID token must still name the original sign-in"

    # A second login is the only way to observe that resolution reused the account.
    again = decode_jwt_claims(_login(fed_client, email, password, provider=hosted_cognito_provider)["access_token"])
    assert again["sub"] == decode_jwt_claims(tokens["access_token"])["sub"], \
        "a repeat login must not create a second account"


@pytest.fixture
def fed_confidential_client(admin_user):
    """A throwaway confidential OAuth client, as the voice assistants use: Alexa authenticates at
    /oauth2/token with HTTP Basic, Google account linking with form-body credentials."""
    client_id = "itest_conf_" + uuid.uuid4().hex[:8]
    created = admin_user.create_oauth_client({
        "client_id": client_id, "client_name": "itest confidential", "client_type": "confidential",
        "redirect_uris": [_FED_REDIRECT_URI], "grant_types": ["authorization_code", "refresh_token"],
        "scopes": ["openid", "email", "phone", "profile"],
        "allowed_resources": [NEO_RESOURCE], "default_resource": NEO_RESOURCE,
    })
    assert created.status_code == 201, created.text
    yield client_id, created.json()["client_secret"]
    admin_user.delete_oauth_client(client_id)


@pytest.mark.xdist_group("env_mut")
def test_confidential_client_authenticates_via_basic_and_body(federated_identity, fed_confidential_client):
    """Both RFC 6749 §2.3.1 client-authentication styles must work at /oauth2/token: HTTP Basic
    (Alexa) and form-body client_secret_post (Google account linking). Google's style regressing is
    invisible in unit terms — the lambda 401s without an error log — so this pins it end to end,
    including the refresh Google performs when its access token expires."""
    _require_fed_env()
    email, _, password = federated_identity
    client_id, client_secret = fed_confidential_client

    for via in ("basic", "post"):
        tokens = _login(client_id, email, password, client_secret=client_secret, secret_via=via)
        assert tokens.get("refresh_token"), f"{via}: token response carried no refresh_token"

        # Google refreshes with body credentials too; assert the same placement works for the grant.
        data = {"grant_type": "refresh_token", "refresh_token": tokens["refresh_token"],
                "client_id": client_id}
        auth = (client_id, client_secret) if via == "basic" else None
        if via == "post":
            data["client_secret"] = client_secret
        refreshed = requests.post(f"{USER_API_GATEWAY_URL}/oauth2/token", data=data, auth=auth)
        assert refreshed.status_code == 200, \
            f"{via}: refresh failed: {refreshed.status_code} {refreshed.text[:300]}"

    # A wrong body secret must still be rejected — body placement must not weaken client auth.
    code = _login(client_id, email, password, code_only=True)
    denied = requests.post(f"{USER_API_GATEWAY_URL}/oauth2/token", data={
        "grant_type": "authorization_code", "code": code, "redirect_uri": _FED_REDIRECT_URI,
        "client_id": client_id, "client_secret": "wrong",
    })
    assert denied.status_code == 401, f"wrong secret must 401: {denied.status_code} {denied.text[:200]}"
    assert "invalid_client" in denied.text


@pytest.mark.xdist_group("env_mut")
def test_email_verified_later_keeps_the_same_account(fed_identity, provision_end_user, fed_client):
    """The account is created by phone alone; verifying an email upstream afterwards must reuse it.

    Without matching on every verified contact this is where a user silently lands in a fresh empty
    account and loses sight of their nodes.
    """
    _require_fed_env()
    email, phone, password = fed_identity["email"], fed_identity["phone"], fed_identity["password"]
    provision_end_user(phone, {"phone_number": phone, "phone_number_verified": "true"}, password)

    by_phone = decode_jwt_claims(_login(fed_client, phone, password)["access_token"])
    assert by_phone.get("phone_number") == phone, by_phone

    boto3.client('cognito-idp', region_name=REGION).admin_update_user_attributes(
        UserPoolId=END_USER_POOL_ID, Username=phone,
        UserAttributes=[{"Name": "email", "Value": email},
                        {"Name": "email_verified", "Value": "true"}],
    )

    after = decode_jwt_claims(_login(fed_client, phone, password)["access_token"])
    assert after["sub"] == by_phone["sub"], \
        "verifying a second contact must not move the user to a new account"
    assert after["email"] == email, f"the new contact must be recorded on the account: {after}"


@pytest.mark.xdist_group("env_mut")
def test_contacts_owned_by_different_accounts_are_refused(fed_identity, provision_end_user, fed_client, hosted_cognito_provider):
    """When each contact already belongs to a different account, the login is refused, not guessed.

    Silently picking one would strand the other account's data or attach the login to the wrong user.
    """
    _require_fed_env()
    email, phone, password = fed_identity["email"], fed_identity["phone"], fed_identity["password"]

    provision_end_user(email, {"email": email, "email_verified": "true"}, password)
    provision_end_user(phone, {"phone_number": phone, "phone_number_verified": "true"}, password)
    _login(fed_client, email, password)
    _login(fed_client, phone, password)

    # The email account now also vouches for the phone, so one login carries contacts owned by two users.
    boto3.client('cognito-idp', region_name=REGION).admin_update_user_attributes(
        UserPoolId=END_USER_POOL_ID, Username=email,
        UserAttributes=[{"Name": "phone_number", "Value": phone},
                        {"Name": "phone_number_verified", "Value": "true"}],
    )

    callback = _login(fed_client, email, password, provider=hosted_cognito_provider, follow_final=True)
    location = callback.headers.get("Location", "")
    assert "code=" not in location, \
        f"a conflicting identity must not yield a code: {callback.status_code} {location}"
    assert callback.status_code == 403, f"one answer for an account we cannot use: {callback.status_code}"



@pytest.mark.xdist_group("env_mut")
def test_scope_gating_withholds_unrequested_claims(federated_identity, fed_client):
    """A login that asks only for openid gets neither the contact nor the profile claims, in the
    tokens or at userinfo — even though the account holds all of them."""
    _require_fed_env()
    email, _, password = federated_identity

    tokens = _login(fed_client, email, password, "openid")
    for label in ("access_token", "id_token"):
        claims = decode_jwt_claims(tokens[label])
        assert claims.get("sub"), f"{label} must still identify the subject: {claims}"
        assert "email" not in claims, f"{label} leaked the email without the scope: {claims}"
        for claim in _UPSTREAM_PROFILE:
            assert claim not in claims, f"{label} leaked {claim} without the profile scope: {claims}"

    info = requests.get(f"{USER_API_GATEWAY_URL}/oauth2/userinfo",
                        headers={"Authorization": f"Bearer {tokens['access_token']}"})
    assert info.status_code == 200, info.text
    reflected = info.json()
    assert reflected.get("sub"), "sub is always present"
    assert "email" not in reflected, f"userinfo leaked the email without the scope: {reflected}"
    for claim in _UPSTREAM_PROFILE:
        assert claim not in reflected, f"userinfo leaked {claim} without the scope: {reflected}"


# ============================================================================
# Authorization-code journey: three products, one browser, one login, one sign-out
# ============================================================================
# The product promise end to end, merged here from the old test_espuser_journey.py. Every other
# espuser test asserts a rule; this asserts the thing a person would describe -- I opened three of
# your apps, signed in once, and signing out of one signed me out of all of them, on this device and
# no other. It re-drives sign-in, single sign-on and sign-out on purpose: a failure in a rule-shaped
# test says a rule regressed, a failure here says the product does not work. A lambda missing one IAM
# grant reads every branch correctly and simply cannot see that sessions exist, so sign-out reaches
# nothing while every rule-shaped test stays green -- only signing in to several products and using
# them after sign-out catches it. Spec: espuser/docs/specs/sso-sessions.md.

_JOURNEY_CALLBACK = "https://example.com/espuser-itest/journey-{}/callback"
_JOURNEY_SIGNED_OUT = "https://example.com/espuser-itest/journey-{}/signed-out"


@pytest.fixture
def three_products(register_espuser_client):
    """Three unrelated applications, as a person's browser would hold: a dashboard and two products
    that know nothing about each other."""
    apps = []
    for name in ("dashboard", "product-a", "product-b"):
        uri, bye = _JOURNEY_CALLBACK.format(name), _JOURNEY_SIGNED_OUT.format(name)
        client = register_espuser_client(
            client_name=f"itest {name}", redirect_uris=[uri], post_logout_redirect_uris=[bye])
        apps.append(WebClient(client.client_id, uri, post_logout_redirect_uri=bye))
    return apps


@pytest.mark.espuser
@requires_espuser
def test_three_products_one_login_one_sign_out(three_products, espuser_person):
    dashboard, product_a, product_b = three_products
    browser = Browser("Mozilla/5.0 (Macintosh) itest-journey")

    # 1 -- the person signs in, once, at whichever app they opened first.
    first = dashboard.sign_in(browser, espuser_person)
    assert first.used_upstream, "the first login must actually authenticate somebody"
    dashboard_tokens = dashboard.tokens(first)
    assert browser.session_cookie, "a completed login must leave a session cookie"

    # 2 -- the other two are already signed in. No page, no provider, no second password.
    tokens = {dashboard.client_id: dashboard_tokens}
    for app in (product_a, product_b):
        authz = app.sign_in(browser, espuser_person)
        assert not authz.used_upstream, \
            f"{app.client_id} went back to the provider; that is not single sign-on"
        tokens[app.client_id] = app.tokens(authz)

    for app in three_products:
        assert app.still_signed_in(tokens[app.client_id]["refresh_token"]), \
            f"{app.client_id} did not end up with working credentials"

    # 3 -- and the person can see exactly that: one browser, three products under it.
    listing = WebClient.sessions(dashboard_tokens["access_token"])
    assert listing.status_code == 200, listing.text
    body = listing.json()
    assert len(body["sessions"]) == 1, f"one browser is one device: {body}"
    view = body["sessions"][0]
    assert view["current"] is True
    assert client_ids_under(view) == sorted(a.client_id for a in three_products), view
    assert not body.get("unattached_products"), \
        f"nothing may be standing access with no session behind it: {body}"

    # 4 -- they sign out of ONE of them, and it reaches all three.
    out = product_a.logout(browser, id_token_hint=tokens[product_a.client_id].get("id_token"))
    assert out.status_code in (200, 302), f"{out.status_code} {out.text[:300]}"
    assert _JOURNEY_SIGNED_OUT.format("product-a") in sign_out_destination(browser, out), (
        "the app that asked must get its person back, however many hops the chain takes: "
        f"{out.headers.get('Location')!r}"
    )

    for app in three_products:
        assert not app.still_signed_in(tokens[app.client_id]["refresh_token"]), (
            f"{app.client_id} can still get new tokens after the sign-out; the person is signed out "
            "of the app they clicked in and still signed in here"
        )

    # 5 -- and the browser itself is signed out: no session, no silent code.
    assert browser.session_cookie is None, "the session cookie must be cleared, not merely stale"
    assert dashboard.authorize(browser, prompt="none").error == "login_required", \
        "the browser still holds a working session; the row was not deleted"


@pytest.mark.espuser
@requires_espuser
def test_sso_logout_the_other_device_is_untouched(three_products, espuser_person):
    """The same journey from two devices, ending in the thing people actually worry about: signing
    out on one machine must not sign them out on the other."""
    dashboard, product_a, _ = three_products
    laptop, phone = Browser("laptop"), Browser("phone")

    laptop_dash = dashboard.tokens(dashboard.sign_in(laptop, espuser_person))
    laptop_a = product_a.tokens(product_a.sign_in(laptop, espuser_person))
    phone_dash = dashboard.tokens(dashboard.sign_in(phone, espuser_person))

    body = WebClient.sessions(phone_dash["access_token"]).json()
    assert len(body["sessions"]) == 2, f"two devices, two rows: {body}"

    product_a.confirm_logout(laptop, product_a.logout(laptop))

    assert not dashboard.still_signed_in(laptop_dash["refresh_token"])
    assert not product_a.still_signed_in(laptop_a["refresh_token"])
    assert dashboard.still_signed_in(phone_dash["refresh_token"]), \
        "the phone was never asked to sign out"

    after = WebClient.sessions(phone_dash["access_token"]).json()
    assert len(after["sessions"]) == 1 and after["sessions"][0]["current"], after


@pytest.mark.espuser
@requires_espuser
def test_sso_logout_lost_phone(three_products, espuser_person):
    """The recovery story, which is the sessions API's whole reason for existing: the device is
    gone, so it cannot be asked to sign itself out. Somebody else's device has to do it, and reaching
    a session from a device that does not hold its cookie is what the by-user index is for."""
    dashboard, product_a, _ = three_products
    laptop, phone = Browser("laptop"), Browser("phone")

    laptop_tokens = dashboard.tokens(dashboard.sign_in(laptop, espuser_person))
    phone_dash = dashboard.tokens(dashboard.sign_in(phone, espuser_person))
    phone_a = product_a.tokens(product_a.sign_in(phone, espuser_person))

    body = WebClient.sessions(laptop_tokens["access_token"]).json()
    phone_sid = [s["session_id"] for s in body["sessions"] if not s["current"]][0]
    phone_view = session_with(body, phone_sid)
    assert client_ids_under(phone_view) == sorted([dashboard.client_id, product_a.client_id]), (
        "the list has to show what is signed in on the lost device, or the person cannot tell which "
        f"one it is: {phone_view}"
    )

    ended = WebClient.end_session(laptop_tokens["access_token"], phone_sid)
    assert ended.status_code == 204, f"{ended.status_code} {ended.text[:200]}"

    assert not dashboard.still_signed_in(phone_dash["refresh_token"])
    assert not product_a.still_signed_in(phone_a["refresh_token"]), \
        "ending a device must take every product on it, not just the one that was listed"
    assert dashboard.still_signed_in(laptop_tokens["refresh_token"]), \
        "and must leave the device that did the ending signed in"

    assert phone_sid not in sids_in(WebClient.sessions(laptop_tokens["access_token"]).json())

    # The phone, if it ever comes back, is a stranger.
    assert dashboard.authorize(phone, prompt="none").error == "login_required", \
        "the ended device still holds a working session cookie"




# ============================================================================
# client_credentials grant (machine-to-machine)
# ============================================================================
# Merged here from the old test_client_credentials.py. The unit specs cover the handler's branches;
# these prove the deployed endpoint behaves the same and that the token it mints is one a resource
# server can act on -- in particular that `aud` names the API and not the caller, the property the
# whole grant exists to get right. The seven near-identical exchanges that once looked at one field
# each are one test now: the deployment cannot make them disagree, so the extra round trips bought
# nothing. That the openid-configuration advertises this grant is asserted in test_oidc_discovery.py.
# Spec: espuser/docs/specs/admin-clients.md, docs/api/User_Api_Swagger.yaml.

_CC_RESOURCE = "https://api.itest-accounts.example.com"
_CC_OTHER_RESOURCE = "https://api.itest-other.example.com"
_CC_SCOPES = ["account.admin", "credits.consume"]


def _cc_claims(access_token):
    """Token payload without verification -- these assert on what the server put in it."""
    payload = access_token.split(".")[1]
    return json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))


@pytest.fixture
def m2m_client(admin_user):
    """A confidential client registered for client_credentials, removed afterwards."""
    client_id = "itest_cc_" + uuid.uuid4().hex[:8]
    create = admin_user.create_oauth_client({
        "client_id": client_id,
        "client_name": "itest m2m",
        "client_type": "confidential",
        "grant_types": ["client_credentials"],
        "scopes": _CC_SCOPES,
        "allowed_resources": [_CC_RESOURCE],
        "default_resource": _CC_RESOURCE,
    })
    assert create.status_code == 201, create.text
    secret = create.json().get("client_secret")
    assert secret, "a confidential client must be issued a secret"
    yield client_id, secret
    admin_user.delete_oauth_client(client_id)


@pytest.mark.espuser
@requires_espuser
def test_client_credentials_mints_one_scoped_access_token(admin_user, m2m_client):
    """One exchange, every property of the response, and the audience binding folded in. There is no
    user, so no refresh_token and no id_token; RFC 9068 s2.2.1 makes sub the client; and `aud` names
    the callee -- the API asked for, else the client's default, and never the client itself, which is
    the bug this grant must not repeat."""
    client_id, secret = m2m_client
    resp = admin_user.oauth_client_credentials(client_id, secret, scope="account.admin")
    assert resp.status_code == 200, resp.text
    body = resp.json()

    assert body["access_token"] and body["token_type"] == "Bearer" and body.get("expires_in")
    assert "refresh_token" not in body, "there is no user to act for later"
    assert "id_token" not in body, "there is no human to describe"

    c = _cc_claims(body["access_token"])
    assert c["sub"] == client_id, "RFC 9068 s2.2.1: with no resource owner, sub is the client"
    assert c["client_id"] == client_id and c["token_use"] == "access"
    assert c["aud"] == _CC_RESOURCE, "no resource requested is the client's default, never the client id"

    # Body credentials must work as well as Basic: Google account linking sends one, Alexa the other,
    # and a regression in either is invisible from the server's own logs.
    assert admin_user.oauth_client_credentials(
        client_id, secret, scope="account.admin", use_basic=False).status_code == 200

    # Omitting scope grants the registered set rather than none.
    everything = _cc_claims(admin_user.oauth_client_credentials(
        client_id, secret).json()["access_token"])
    assert sorted(everything["scope"].split()) == sorted(_CC_SCOPES)

    # A requested resource becomes the audience -- the one property whose failure means tokens are
    # usable at the wrong API.
    scoped = _cc_claims(admin_user.oauth_client_credentials(
        client_id, secret, scope="account.admin", resource=_CC_RESOURCE).json()["access_token"])
    assert scoped["aud"] == _CC_RESOURCE and scoped["aud"] != scoped["client_id"]


@pytest.mark.espuser
@requires_espuser
def test_client_credentials_refuses_an_unregistered_scope_or_resource(admin_user, m2m_client):
    """The target validation, without which the scope and resource parameters would be decorative.
    An unregistered scope is invalid_scope; an unregistered resource, or more than one resource (one
    resource, one aud -- a multi-audience token is one leak that opens two doors), is invalid_target."""
    client_id, secret = m2m_client

    bad_scope = admin_user.oauth_client_credentials(client_id, secret, scope="nope.write")
    assert bad_scope.status_code == 400 and bad_scope.json()["error"] == "invalid_scope", bad_scope.text

    bad_resource = admin_user.oauth_client_credentials(
        client_id, secret, scope="account.admin", resource=_CC_OTHER_RESOURCE)
    assert bad_resource.status_code == 400 and bad_resource.json()["error"] == "invalid_target", \
        bad_resource.text

    two = admin_user.oauth_client_credentials(
        client_id, secret, scope="account.admin",
        resource=_CC_RESOURCE, extra_resources=[_CC_OTHER_RESOURCE])
    assert two.status_code == 400 and two.json()["error"] == "invalid_target", two.text


@pytest.mark.espuser
@requires_espuser
def test_client_credentials_refuses_a_client_not_registered_for_the_grant(admin_user):
    """Registered as confidential, but only for authorization_code -- the grant gate must hold
    independently of whether the client could authenticate."""
    client_id = "itest_cc_nogrant_" + uuid.uuid4().hex[:8]
    create = admin_user.create_oauth_client({
        "client_id": client_id, "client_name": "itest authcode only",
        "client_type": "confidential", "grant_types": ["authorization_code"],
    })
    assert create.status_code == 201, create.text
    secret = create.json()["client_secret"]
    try:
        resp = admin_user.oauth_client_credentials(client_id, secret)
        assert resp.status_code == 400 and resp.json()["error"] == "unauthorized_client", resp.text
    finally:
        admin_user.delete_oauth_client(client_id)
