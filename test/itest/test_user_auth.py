# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from py_sdk.test_user import User
from test.itest.conftest import (
    USER_API_GATEWAY_URL,
    REGION,
    IDENTITY_POOL_ID,
    API_GATEWAY_URL,
    IOT_ENDPOINT,
    ADMIN_USER_POOL_ID,
    ADMIN_CLIENT_ID,
    END_USER_POOL_ID,
    complete_federation_login,
    decode_jwt_claims,
)
from test.itest.email_utils import (
    generate_random_email,
    generate_test_password,
)
from urllib.parse import parse_qs, urlparse
import boto3
import pytest
import requests
import uuid




def _new_user(email):
    """A bare, unauthenticated User helper bound to an email (no signin)."""
    user = User(email, "unused", REGION, IDENTITY_POOL_ID,
                API_GATEWAY_URL, USER_API_GATEWAY_URL, IOT_ENDPOINT)
    user.mailosaur_email = email
    return user




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
                 is_super_admin=True)
    end_user = User(email, password, REGION, IDENTITY_POOL_ID, API_GATEWAY_URL, USER_API_GATEWAY_URL,
                    IOT_ENDPOINT)
    end_user.end_user_pool_id = END_USER_POOL_ID
    try:
        assert admin.create_super_admin_via_cognito(email=email, password=password), \
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


def test_revocation_ends_the_login_on_both_surfaces(test_user1):
    """Revoking ends a login wherever it is asked: the RFC 7009 endpoint and the legacy signout both
    leave the presented refresh token unredeemable."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    first = test_user1.signin()
    assert first.status_code == 200, first.text
    token = first.json()["refresh_token"]
    assert test_user1.oauth_revoke(token, token_type_hint="refresh_token").status_code == 200
    assert test_user1.refresh_tokens(token).status_code == 400, "a revoked token must not redeem"

    # A fresh login, ended through the legacy surface instead.
    second = test_user1.signin()
    assert second.status_code == 200, second.text
    legacy_token = second.json()["refresh_token"]
    assert test_user1.signout(refresh_token=legacy_token).status_code == 200
    assert test_user1.refresh_tokens(legacy_token).status_code != 200, \
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
    assert rejected.status_code == 403, \
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
def fed_client(super_admin_user):
    """A throwaway OAuth client with our redirect_uri registered.

    The seeded first-party clients carry no redirect_uris, so /oauth2/authorize refuses them — a
    federated login needs a client that names where the code may be sent.
    """
    client_id = "itest_fed_" + uuid.uuid4().hex[:8]
    created = super_admin_user.create_oauth_client({
        "client_id": client_id, "client_name": "itest federation", "client_type": "public",
        "redirect_uris": [_FED_REDIRECT_URI], "grant_types": ["authorization_code", "refresh_token"],
        "scopes": ["openid", "email", "phone", "profile"], "require_pkce": True,
    })
    assert created.status_code == 201, created.text
    yield client_id
    super_admin_user.delete_oauth_client(client_id)


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
def test_federated_login_carries_contacts_and_claims(federated_identity, fed_client):
    """One brokered login, asserted end to end: both verified contacts are stored, the upstream
    profile reaches both tokens, userinfo agrees with them, a refresh preserves them, and a second
    login reuses the account rather than creating another."""
    _require_fed_env()
    email, phone, password = federated_identity

    tokens = _login(fed_client, email, password, "openid email phone profile")

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
    refreshed_claims = decode_jwt_claims(refreshed.json()["access_token"])
    for claim, expected in _UPSTREAM_PROFILE.items():
        assert refreshed_claims.get(claim) == expected, f"refreshed token lost {claim}: {refreshed_claims}"

    # A second login is the only way to observe that resolution reused the account.
    again = decode_jwt_claims(_login(fed_client, email, password)["access_token"])
    assert again["sub"] == decode_jwt_claims(tokens["access_token"])["sub"], \
        "a repeat login must not create a second account"


@pytest.fixture
def fed_confidential_client(super_admin_user):
    """A throwaway confidential OAuth client, as the voice assistants use: Alexa authenticates at
    /oauth2/token with HTTP Basic, Google account linking with form-body credentials."""
    client_id = "itest_conf_" + uuid.uuid4().hex[:8]
    created = super_admin_user.create_oauth_client({
        "client_id": client_id, "client_name": "itest confidential", "client_type": "confidential",
        "redirect_uris": [_FED_REDIRECT_URI], "grant_types": ["authorization_code", "refresh_token"],
        "scopes": ["openid", "email", "phone", "profile"],
    })
    assert created.status_code == 201, created.text
    yield client_id, created.json()["client_secret"]
    super_admin_user.delete_oauth_client(client_id)


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
    cb = _login(client_id, email, password, follow_final=True)
    code = parse_qs(urlparse(cb.headers["Location"]).query)["code"][0]
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
def test_contacts_owned_by_different_accounts_are_refused(fed_identity, provision_end_user, fed_client):
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

    callback = _login(fed_client, email, password, follow_final=True)
    location = callback.headers.get("Location", "")
    assert "code=" not in location, \
        f"a conflicting identity must not yield a code: {callback.status_code} {location}"



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
