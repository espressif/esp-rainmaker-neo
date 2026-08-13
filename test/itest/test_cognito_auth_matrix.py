# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""The legacy /v1/user/auth/* surface against both provider shapes it must serve.

Every test here runs once per `cognito_pool` param:

  inbuilt         the pool this deployment provisions (EspEndUserPoolId), reached through the
                  provider row the stack seeds.
  external        a pool this suite creates itself, shaped like the production 3P pool
                  (UsernameAttributes = email + phone_number, so email IS the username) and
                  registered as a password-grant provider row for the duration of the module.
An AliasAttributes pool is NOT a param: Cognito rejects email-format usernames outright on that
shape ("Username cannot be of email format, since user pool is configured for email alias"), and
this surface signs up with the email as the username — so the whole API is unusable against such
a pool. test_alias_shaped_pool_is_unsupported pins that incompatibility.

The external case exists because the backend resolves its password provider from the
identity-providers registry at request time, so a pool in another account is a
configuration, not a deployment. Both shapes must behave identically — same statuses, same
side effects, same masking — and a divergence in any of that is the bug this suite is for.

The pool is created per module and deleted afterwards, so no test ever writes to a real
pool and no verification mail reaches a real address.

The signup 201 message is the one response that differs between the two, mirroring
signupMessage in espuser/handlers/user_auth. Every failure message is identical across both.

Two properties are asserted throughout, for an existing and a non-existing user on every
endpoint:

  the answer      exact status and body, because these responses are deliberately uniform
                  and a "clearer" message would be an enumeration oracle.
  the side effect  whether a verification code actually arrives, read from the inbox. A
                  masked response that still mails a code leaks existence out of band, so
                  the response assertion alone would not catch it.
"""

from test.itest.conftest import (
    USER_API_GATEWAY_URL,
    REGION,
    END_USER_POOL_ID,
)
from test.itest.email_utils import (
    generate_mailosaur_email,
    generate_random_email,
    generate_test_password,
    get_verification_code_from_server,
)
from botocore.exceptions import ClientError
import boto3
import pytest
import requests
import time
import uuid

# This suite is opt-in: it makes many real signup calls that burn the pool's daily Cognito email
# quota, so the default `make itest` deselects it (`-m "not ... and not cognito"`). Run it on its
# own with `make itest ITEST_ARGS='-n 12 -m cognito'` (or `-k cognito_auth_matrix`).
#
# xdist_group("env_mut"): registering the external provider row swaps the password provider for the
# whole deployment, so no test in this module may run beside another that touches the auth surface.
# The group covers the module rather than the mutating tests alone because the external param makes
# every one of them a mutator.
pytestmark = [pytest.mark.cognito, pytest.mark.xdist_group("env_mut")]

# A pool this suite creates has no Lambda triggers and no SES sender, so Cognito mails the
# code itself from its default address. That is the same delivery path the inbuilt pool
# uses for signup verification, which is what these tests read.
PROVIDER_TABLE = "espuser-identity-providers"
TEST_PROVIDER_NAME = "itest-external-cognito"
ALIAS_PROVIDER_NAME = "itest-external-cognito-alias"
INBUILT_PROVIDER_NAME = "cognito"

# provider row backing each cognito_pool param
PROVIDER_ROW = {
    "inbuilt": INBUILT_PROVIDER_NAME,
    "external": TEST_PROVIDER_NAME,
    "external-alias": ALIAS_PROVIDER_NAME,
}

# Long enough for Cognito to mail a code and Mailosaur to receive it.
CODE_WAIT = {"max_retries": 6, "retry_delay": 3.0, "timeout": 45.0}
# Asserting a code does NOT arrive: short, because it must fail fast to be worth running.
NO_CODE_WAIT = {"max_retries": 2, "retry_delay": 5.0, "timeout": 20.0}

# Seconds to let a provider-row swap become visible to the lambda's eventually-consistent Scan.
PROVIDER_SWAP_SETTLE = 2.0


UNCONFIRMED_ACCOUNT_MESSAGE = "Signin failed. Account not verified — reset your password."

SIGNUP_MESSAGES = {
    # Neither the bundled "cognito" provider nor the test-external one is named "rainmaker",
    # so both resolve to the neutral wording. The RainMaker-branded variant is unit-tested only.
    "inbuilt": "Code sent. Existing users must signin or reset password.",
    "external": "Code sent. Existing users must signin or reset password.",
}

# Signup by someone who proved the password of an existing confirmed account, mirroring
# accountExistsMessage in the handler. The one 201 body that differs — reachable only with the
# credential, so it is no enumeration oracle.
ACCOUNT_EXISTS_MESSAGES = {
    "inbuilt": "Account already exists. Signin or reset password.",
    "external": "Account already exists. Signin or reset password.",
}

# One answer for every /signup/verify failure, mirroring verifyFailedMessage in the handler.
VERIFY_FAILED_MESSAGES = {
    "inbuilt": "Invalid code or account already exists. Try signin or reset password.",
    "external": "Invalid code or account already exists. Try signin or reset password.",
}


def _auth_url(path):
    return f"{USER_API_GATEWAY_URL}/v1/user/auth/{path}"


def _require_outputs():
    """Skip when the espuser stack outputs this suite drives are not configured."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("espuser outputs not configured")


def _status(response):
    """The message the handlers wrap every status in, or None for a token body.

    The wire field is `message`: utils.NewAPIStatus marshals APIStatus.Message as `message`.
    """
    try:
        return response.json().get("message")
    except ValueError:
        return None


# ============================================================================
# The external pool: created here, torn down here.
# ============================================================================


def _create_external_pool(cognito, alias_shaped=False):
    """A pool shaped like the production 3P pool in the ways this surface depends on.

    UsernameAttributes is the load-bearing setting: it makes the email the account's own
    username, so a duplicate signup raises UsernameExistsException. A pool using
    AliasAttributes instead would raise AliasExistsException, which the backend treats the
    same but Cognito does not — see test_duplicate_signup_raises_username_exists.
    """
    shape = ({"AliasAttributes": ["email", "phone_number"]} if alias_shaped
             else {"UsernameAttributes": ["email", "phone_number"]})
    pool = cognito.create_user_pool(
        PoolName=f"rmng-itest-{uuid.uuid4().hex[:12]}",
        Policies={"PasswordPolicy": {
            "MinimumLength": 8, "RequireUppercase": True, "RequireLowercase": True,
            "RequireNumbers": True, "RequireSymbols": False,
        }},
        AutoVerifiedAttributes=["email"],
        Schema=[{"Name": "email", "AttributeDataType": "String",
                 "Mutable": True, "Required": False}],
        **shape,
    )["UserPool"]

    client = cognito.create_user_pool_client(
        UserPoolId=pool["Id"],
        ClientName="rmng-itest-client",
        GenerateSecret=True,
        ExplicitAuthFlows=["ALLOW_USER_PASSWORD_AUTH", "ALLOW_REFRESH_TOKEN_AUTH",
                           "ALLOW_USER_SRP_AUTH"],
        # The prod client sets this; keeping it identical means Cognito masks existence
        # errors here too, so the backend's own masking is what these tests measure.
        PreventUserExistenceErrors="ENABLED",
    )["UserPoolClient"]

    return pool["Id"], client["ClientId"], client["ClientSecret"]


def _providers_table():
    return boto3.resource("dynamodb", region_name=REGION).Table(PROVIDER_TABLE)


def _set_provider_enabled(provider_name, enabled):
    """Enable or disable one provider row, without creating it if it is absent.

    NewService takes the first enabled row offering a password grant out of a DynamoDB Scan, whose
    order is unspecified, and both rows here offer one. So exactly one of them must be enabled
    whenever a request is made, which is what _select_pool below guarantees per test.
    """
    try:
        _providers_table().update_item(
            Key={"provider_name": provider_name},
            UpdateExpression="SET enabled = :e",
            ExpressionAttributeValues={":e": enabled},
            ConditionExpression="attribute_exists(provider_name)",
        )
    except ClientError as e:  # a row that does not exist needs no disabling
        if e.response["Error"]["Code"] != "ConditionalCheckFailedException":
            raise


def _select_pool(label):
    """Make `label`'s pool the only one the legacy surface can resolve to.

    Per test, not per module: pytest interleaves the params, so a module-scoped swap would leave
    every later test of another param resolving to the wrong pool — silently testing one pool
    against fixtures created in another.
    """
    wanted = PROVIDER_ROW[label]
    for name in PROVIDER_ROW.values():
        if name != wanted:
            _set_provider_enabled(name, False)
    _set_provider_enabled(wanted, True)
    # The lambda re-reads the registry per request, but the Scan behind it is eventually
    # consistent, so give the swap a moment to be visible before the test drives the API.
    time.sleep(PROVIDER_SWAP_SETTLE)


def _register_provider(provider_name, pool_id, client_id, client_secret):
    """Point the legacy surface at the pool by writing its provider row.

    The row must be removed again in teardown or it would shadow the deployment's own provider for
    every later test.
    """
    issuer = f"https://cognito-idp.{REGION}.amazonaws.com/{pool_id}"
    _providers_table().put_item(Item={
        "provider_name": provider_name,
        "type": "oidc",
        "display_name": "itest external cognito",
        "enabled": False,
        "issuer": issuer,
        "client_id": client_id,
        "client_secret": client_secret,
        "jwks_url": f"{issuer}/.well-known/jwks.json",
        "scopes": "openid email phone",
        "password_grant": True,
        "token_endpoint_auth": "client_secret_basic",
        "created_at": int(time.time()),
    })


@pytest.fixture(scope="module")
def external_pool():
    """Create the throwaway pool, register it, and undo both afterwards."""
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")
    cognito = boto3.client("cognito-idp", region_name=REGION)
    try:
        pool_id, client_id, client_secret = _create_external_pool(cognito)
    except Exception as e:  # noqa: BLE001 — no permission to create a pool is a skip, not a failure
        pytest.skip(f"cannot create a test user pool: {e}")

    _register_provider(TEST_PROVIDER_NAME, pool_id, client_id, client_secret)
    try:
        yield pool_id
    finally:
        try:
            _set_provider_enabled(INBUILT_PROVIDER_NAME, True)
        except Exception:  # noqa: BLE001
            pass
        try:
            _providers_table().delete_item(Key={"provider_name": TEST_PROVIDER_NAME})
        except Exception:  # noqa: BLE001
            pass
        try:
            cognito.delete_user_pool(UserPoolId=pool_id)
        except Exception:  # noqa: BLE001
            pass


@pytest.fixture(scope="module")
def external_alias_pool():
    """A pool using AliasAttributes instead of UsernameAttributes — the "alias trap" shape.

    Here the username is separate from the email, so a second account can be created for an
    address that is already taken: Cognito accepts the SignUp and only rejects it at
    ConfirmSignUp, with AliasExistsException. A deployment can be pointed at such a pool by an
    operator registering their own provider, so the surface has to answer sanely on it.
    """
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")
    cognito = boto3.client("cognito-idp", region_name=REGION)
    try:
        pool_id, client_id, client_secret = _create_external_pool(cognito, alias_shaped=True)
    except Exception as e:  # noqa: BLE001 — no permission to create a pool is a skip, not a failure
        pytest.skip(f"cannot create an alias-shaped test user pool: {e}")

    _register_provider(ALIAS_PROVIDER_NAME, pool_id, client_id, client_secret)
    try:
        yield pool_id
    finally:
        try:
            _set_provider_enabled(INBUILT_PROVIDER_NAME, True)
        except Exception:  # noqa: BLE001
            pass
        try:
            _providers_table().delete_item(Key={"provider_name": ALIAS_PROVIDER_NAME})
        except Exception:  # noqa: BLE001
            pass
        try:
            cognito.delete_user_pool(UserPoolId=pool_id)
        except Exception:  # noqa: BLE001
            pass


@pytest.fixture(params=["inbuilt", "external"])
def cognito_pool(request):
    """The pool under test, as (label, pool_id).

    Both params drive the same public API; only which pool backs it differs. The external
    param pulls in the module fixture lazily so the pool is created only when that param
    actually runs (a `-k inbuilt` run creates nothing).

    Every test taking this fixture is in the env_mut xdist group (see the module-level
    pytestmark): registering the external provider row replaces the password provider the
    whole deployment resolves, so nothing else may run against the auth surface meanwhile.
    """
    _require_outputs()
    if request.param == "inbuilt":
        if not END_USER_POOL_ID:
            pytest.skip("espuser outputs not configured")
        _select_pool("inbuilt")
        return "inbuilt", END_USER_POOL_ID
    pool_id = request.getfixturevalue("external_pool")
    _select_pool("external")
    return "external", pool_id


# ============================================================================
# Users. A confirmed account and an address that has never been used.
# ============================================================================


@pytest.fixture
def confirmed_user(cognito_pool):
    """An account that exists and is confirmed, created straight through the admin API.

    Admin-created and admin-confirmed rather than signed-up-and-verified: the account only
    needs to *exist* here, and going through the mail flow would spend a code that the
    tests reading the inbox would then have to disambiguate.
    """
    _, pool_id = cognito_pool
    cognito = boto3.client("cognito-idp", region_name=REGION)
    email = generate_random_email()
    password = generate_test_password()
    cognito.admin_create_user(
        UserPoolId=pool_id, Username=email, MessageAction="SUPPRESS",
        UserAttributes=[{"Name": "email", "Value": email},
                        {"Name": "email_verified", "Value": "true"}],
    )
    cognito.admin_set_user_password(
        UserPoolId=pool_id, Username=email, Password=password, Permanent=True)
    try:
        yield email, password
    finally:
        try:
            cognito.admin_delete_user(UserPoolId=pool_id, Username=email)
        except Exception:  # noqa: BLE001
            pass


@pytest.fixture
def unconfirmed_user(cognito_pool):
    """An account created through the signup API and deliberately left unverified.

    Made by the API rather than admin_create_user because only the signup path produces
    UNCONFIRMED; an admin-created user lands in FORCE_CHANGE_PASSWORD, which behaves differently
    at both /token and /signup/verify.
    """
    _, pool_id = cognito_pool
    email = generate_mailosaur_email() or generate_random_email()
    password = generate_test_password()
    created = requests.post(_auth_url("signup"), json={"email": email, "password": password})
    if created.status_code != 201:
        pytest.skip(f"could not create an unconfirmed account: {created.text}")
    try:
        yield email, password
    finally:
        try:
            boto3.client("cognito-idp", region_name=REGION).admin_delete_user(
                UserPoolId=pool_id, Username=email)
        except Exception:  # noqa: BLE001
            pass


@pytest.fixture
def absent_user():
    """An address with no account anywhere — never signed up, never admin-created."""
    return generate_random_email(), generate_test_password()


# ============================================================================
# signup — the existence answer, and whether a code follows it.
# ============================================================================


def test_signup_answers_alike_for_new_and_existing(cognito_pool, confirmed_user):
    """A new address and an existing one probed with the WRONG password get byte-identical 201s.

    Comparing whole bodies, not just the status: a field that differed (a user id, a
    "requires_verification" that flipped) would be an oracle even with the status equal. The wrong
    password is deliberate — it is the case a guesser can reach, and it must reveal nothing. The
    correct-password case is the one allowed exception, asserted in
    test_signup_of_a_confirmed_owner_is_routed_to_signin.
    """
    label, _ = cognito_pool
    existing_email, _ = confirmed_user

    fresh = requests.post(_auth_url("signup"),
                          json={"email": generate_random_email(), "password": generate_test_password()})
    existing = requests.post(_auth_url("signup"),
                             json={"email": existing_email, "password": generate_test_password()})

    assert fresh.status_code == 201, f"[{label}] new signup: {fresh.text}"
    assert existing.status_code == 201, f"[{label}] existing signup: {existing.text}"
    assert fresh.json()["message"] == SIGNUP_MESSAGES[label], f"[{label}] {fresh.text}"
    assert existing.text == fresh.text, (
        f"[{label}] a wrong-password duplicate must not reveal the account exists: "
        f"{existing.text} vs {fresh.text}"
    )


def test_signup_delivers_a_code_to_a_new_address(cognito_pool, require_mailosaur):
    """The success this endpoint claims is real: a first-time signup does mail a code, and
    that code verifies the account. Without this, the no-code assertions below would also
    pass on a pool that never mails anything."""
    label, pool_id = cognito_pool
    email = generate_mailosaur_email()
    if not email:
        pytest.skip("Mailosaur address unavailable")
    cognito = boto3.client("cognito-idp", region_name=REGION)

    started = time.time()
    created = requests.post(_auth_url("signup"),
                            json={"email": email, "password": generate_test_password()})
    assert created.status_code == 201, f"[{label}] {created.text}"
    try:
        code = get_verification_code_from_server(
            recipient_email=email, since_timestamp=started, **CODE_WAIT)
        assert code, f"[{label}] a new signup must deliver a verification code"

        verified = requests.post(_auth_url("signup/verify"),
                                 json={"email": email, "code": code})
        assert verified.status_code == 200, f"[{label}] {verified.text}"
        assert _status(verified) == "Verified successfully. You can now login.", verified.text
    finally:
        try:
            cognito.admin_delete_user(UserPoolId=pool_id, Username=email)
        except Exception:  # noqa: BLE001
            pass


def test_signup_of_a_confirmed_owner_is_routed_to_signin(cognito_pool, require_mailosaur):
    """A duplicate signup whose password authenticates a confirmed account is told to sign in,
    and NO code is mailed.

    This is the one 201 whose body differs, and the reason it is safe: InitiateAuth authenticated,
    so the caller already holds the credential — the message discloses nothing a guesser could
    reach. The inbox side is the real assertion: the old behaviour mailed a confirmed user a code
    they could not use, and this proves it no longer does.
    """
    label, pool_id = cognito_pool
    email = generate_mailosaur_email()
    if not email:
        pytest.skip("Mailosaur address unavailable")
    password = generate_test_password()
    cognito = boto3.client("cognito-idp", region_name=REGION)
    cognito.admin_create_user(
        UserPoolId=pool_id, Username=email, MessageAction="SUPPRESS",
        UserAttributes=[{"Name": "email", "Value": email},
                        {"Name": "email_verified", "Value": "true"}])
    cognito.admin_set_user_password(
        UserPoolId=pool_id, Username=email, Password=password, Permanent=True)
    try:
        started = time.time()
        again = requests.post(_auth_url("signup"), json={"email": email, "password": password})
        assert again.status_code == 409, f"[{label}] {again.text}"
        assert _status(again) == ACCOUNT_EXISTS_MESSAGES[label], f"[{label}] {again.text}"

        assert get_verification_code_from_server(
            recipient_email=email, since_timestamp=started, **NO_CODE_WAIT) is None, \
            f"[{label}] a confirmed owner must not be mailed a verification code"
    finally:
        try:
            cognito.admin_delete_user(UserPoolId=pool_id, Username=email)
        except Exception:  # noqa: BLE001
            pass


def test_signup_resends_a_code_to_an_unconfirmed_account(cognito_pool, require_mailosaur):
    """The other half of the rule above: an account that exists but never verified is
    entitled to another code, because that is the legitimate retry path for a lost mail.
    Suppressing it here would be the enumeration fix breaking real signups."""
    label, pool_id = cognito_pool
    email = generate_mailosaur_email()
    if not email:
        pytest.skip("Mailosaur address unavailable")
    password = generate_test_password()
    cognito = boto3.client("cognito-idp", region_name=REGION)

    started = time.time()
    first = requests.post(_auth_url("signup"), json={"email": email, "password": password})
    assert first.status_code == 201, f"[{label}] {first.text}"
    try:
        assert get_verification_code_from_server(
            recipient_email=email, since_timestamp=started, **CODE_WAIT), \
            f"[{label}] the first signup must deliver a code"

        # Deliberately NOT verifying, so the account stays UNCONFIRMED.
        retried_at = time.time()
        again = requests.post(_auth_url("signup"), json={"email": email, "password": password})
        assert again.status_code == 201, f"[{label}] {again.text}"
        assert again.text == first.text, f"[{label}] the body must stay uniform"

        resent = get_verification_code_from_server(
            recipient_email=email, since_timestamp=retried_at, **CODE_WAIT)
        assert resent, f"[{label}] an unconfirmed account must be able to get another code"
    finally:
        try:
            cognito.admin_delete_user(UserPoolId=pool_id, Username=email)
        except Exception:  # noqa: BLE001
            pass


def test_duplicate_signup_raises_username_exists(cognito_pool, confirmed_user):
    """Which Cognito exception a duplicate signup actually raises, asserted at the pool.

    The backend collapses UsernameExistsException and AliasExistsException into one branch,
    but only the former guarantees that a resend is refused for a confirmed account. Both
    pools here set UsernameAttributes (email IS the username), so the exception must be
    UsernameExists — if a pool were ever reconfigured to use AliasAttributes instead, this
    fails and the collapsed branch above stops being safe.
    """
    label, pool_id = cognito_pool
    email, password = confirmed_user
    cognito = boto3.client("cognito-idp", region_name=REGION)

    described = cognito.describe_user_pool(UserPoolId=pool_id)["UserPool"]
    assert "email" in (described.get("UsernameAttributes") or []), (
        f"[{label}] this pool's reasoning assumes email is the username; "
        f"UsernameAttributes={described.get('UsernameAttributes')} "
        f"AliasAttributes={described.get('AliasAttributes')}"
    )

    clients = cognito.list_user_pool_clients(UserPoolId=pool_id, MaxResults=10)["UserPoolClients"]
    assert clients, f"[{label}] the pool has no app client"
    client_id = clients[0]["ClientId"]
    described_client = cognito.describe_user_pool_client(
        UserPoolId=pool_id, ClientId=client_id)["UserPoolClient"]

    with pytest.raises(cognito.exceptions.UsernameExistsException):
        kwargs = {"ClientId": client_id, "Username": email, "Password": password,
                  "UserAttributes": [{"Name": "email", "Value": email}]}
        if described_client.get("ClientSecret"):
            import base64
            import hashlib
            import hmac
            digest = hmac.new(described_client["ClientSecret"].encode(),
                              (email + client_id).encode(), hashlib.sha256).digest()
            kwargs["SecretHash"] = base64.b64encode(digest).decode()
        cognito.sign_up(**kwargs)


# ============================================================================
# signup/verify — every failure is one answer.
# ============================================================================


@pytest.mark.parametrize("case,code", [
    ("wrong-code", "000000"),
    ("expired-shaped-code", "123456"),
    ("empty-code", ""),
])
def test_verify_rejects_every_bad_code_identically(cognito_pool, unconfirmed_user, case, code):
    """A wrong code against a real *not-yet-verified* account, and against an account that does
    not exist, are the same 400 with the same body — a distinguishable "no such user" would turn
    this endpoint into an enumeration oracle.

    The existing account here is UNCONFIRMED on purpose. A confirmed one is the single state this
    endpoint deliberately names (see test_verify_of_a_confirmed_account_says_to_sign_in), so using
    it here would assert the opposite of the intended behaviour.
    """
    label, _ = cognito_pool
    existing_email, _ = unconfirmed_user
    absent_email = generate_random_email()

    on_existing = requests.post(_auth_url("signup/verify"),
                                json={"email": existing_email, "code": code})
    on_absent = requests.post(_auth_url("signup/verify"),
                              json={"email": absent_email, "code": code})

    assert on_existing.status_code == 400, f"[{label}/{case}] existing: {on_existing.text}"
    assert on_absent.status_code == 400, f"[{label}/{case}] absent: {on_absent.text}"
    assert _status(on_existing) == VERIFY_FAILED_MESSAGES[label], on_existing.text
    assert _status(on_absent) == VERIFY_FAILED_MESSAGES[label], on_absent.text
    assert on_existing.text == on_absent.text, (
        f"[{label}/{case}] existing and absent must be indistinguishable: "
        f"{on_existing.text} vs {on_absent.text}"
    )


def test_verify_of_a_confirmed_account_fails_with_the_same_message(cognito_pool, confirmed_user):
    """Re-verifying a confirmed account gets the same uniform 400 as any bad code.

    Verified against a real pool: Cognito validates the code BEFORE the account state
    (CodeMismatchException for a wrong code, ExpiredCodeException for a spent one), so this
    endpoint cannot distinguish an already-confirmed account, and no message may pretend to.
    The uniform message itself carries the sign-in/reset hint, which is what routes a user who
    signed up twice without disclosing anything: everyone sees the same words.
    """
    label, _ = cognito_pool
    email, _ = confirmed_user

    replayed = requests.post(_auth_url("signup/verify"),
                             json={"email": email, "code": "123456"})
    assert replayed.status_code == 400, f"[{label}] {replayed.text}"
    assert _status(replayed) == VERIFY_FAILED_MESSAGES[label], replayed.text

    # And byte-identical to the answer an absent user gets, which is the enumeration property.
    on_absent = requests.post(_auth_url("signup/verify"),
                              json={"email": generate_random_email(), "code": "123456"})
    assert replayed.text == on_absent.text, (
        f"[{label}] confirmed and absent must be indistinguishable: "
        f"{replayed.text} vs {on_absent.text}"
    )


def test_alias_shaped_pool_is_unsupported(external_alias_pool):
    """An AliasAttributes pool cannot serve this surface at all, and the failure is clean.

    This surface signs up with the email AS the username, and Cognito refuses email-format
    usernames on an alias pool ("Username cannot be of email format, since user pool is
    configured for email alias") — so signup answers the masked 400 rather than 201. Pinned so an
    operator registering an alias-shaped provider gets a diagnosable failure, not a half-working
    deployment.
    """
    _select_pool("external-alias")
    refused = requests.post(_auth_url("signup"),
                            json={"email": generate_random_email(),
                                  "password": generate_test_password()})
    assert refused.status_code == 400, refused.text
    assert _status(refused) == "Failed to create user account", refused.text


# ============================================================================
# token — signin, for an account that exists and one that does not.
# ============================================================================


def test_signin_succeeds_for_a_confirmed_account(cognito_pool, confirmed_user):
    """The happy path, so the uniform-401 tests below cannot pass by rejecting everything."""
    label, _ = cognito_pool
    email, password = confirmed_user

    signed_in = requests.post(_auth_url("token"),
                              json={"username": email, "password": password})
    assert signed_in.status_code == 200, f"[{label}] {signed_in.text}"
    body = signed_in.json()
    for field in ("access_token", "refresh_token", "id_token"):
        assert body.get(field), f"[{label}] missing {field}: {signed_in.text}"
    assert body.get("token_type", "").lower() == "bearer", signed_in.text


def test_signin_of_an_unconfirmed_account_asks_for_a_password_reset(cognito_pool):
    """An account that signed up but never verified is told so by name, with the recovery
    instruction — the one sign-in answer that is deliberately NOT the uniform refusal.

    The account must be made by the signup API, not admin_create_user: the latter lands in
    FORCE_CHANGE_PASSWORD, which answers a NEW_PASSWORD_REQUIRED challenge rather than the
    UserNotConfirmedException this behaviour keys on.

    Cognito raises that exception only once the password matches, so the disclosure is limited to
    a caller who already holds the credential; the wrong-password case below stays uniform. That
    is asserted here too, because it is the boundary the whole enumeration story rests on.
    """
    label, pool_id = cognito_pool
    email = generate_mailosaur_email() or generate_random_email()
    password = generate_test_password()
    cognito = boto3.client("cognito-idp", region_name=REGION)

    created = requests.post(_auth_url("signup"), json={"email": email, "password": password})
    assert created.status_code == 201, f"[{label}] {created.text}"
    try:
        refused = requests.post(_auth_url("token"),
                                json={"username": email, "password": password})
        assert refused.status_code == 401, f"[{label}] {refused.text}"
        assert _status(refused) == UNCONFIRMED_ACCOUNT_MESSAGE, refused.text

        # The same unconfirmed account, wrong password: back to the uniform refusal, so this
        # message cannot be used to test whether an address is registered.
        guessed = requests.post(_auth_url("token"),
                                json={"username": email, "password": generate_test_password()})
        assert guessed.status_code == 401, f"[{label}] {guessed.text}"
        assert _status(guessed) == "Authentication failed", guessed.text
    finally:
        try:
            cognito.admin_delete_user(UserPoolId=pool_id, Username=email)
        except Exception:  # noqa: BLE001
            pass


@pytest.mark.parametrize("case", ["wrong-password", "absent-user"])
def test_signin_fails_identically_for_bad_password_and_absent_user(
        cognito_pool, confirmed_user, absent_user, case):
    """A real account with the wrong password and an account that was never created get the
    same 401 and the same body — the response says nothing about which of the two it was."""
    label, _ = cognito_pool
    existing_email, _ = confirmed_user
    absent_email, _ = absent_user
    wrong = generate_test_password()

    username = existing_email if case == "wrong-password" else absent_email
    refused = requests.post(_auth_url("token"),
                            json={"username": username, "password": wrong})

    assert refused.status_code == 401, f"[{label}/{case}] {refused.text}"
    assert _status(refused) == "Authentication failed", refused.text


def test_signin_bodies_match_across_bad_password_and_absent_user(cognito_pool, confirmed_user):
    """The two refusals compared directly, since equality is the property under test."""
    label, _ = cognito_pool
    existing_email, _ = confirmed_user

    on_existing = requests.post(_auth_url("token"),
                                json={"username": existing_email,
                                      "password": generate_test_password()})
    on_absent = requests.post(_auth_url("token"),
                              json={"username": generate_random_email(),
                                    "password": generate_test_password()})
    assert on_existing.text == on_absent.text, (
        f"[{label}] a wrong password and an absent user must be indistinguishable: "
        f"{on_existing.text} vs {on_absent.text}"
    )


# ============================================================================
# password-recovery — the masked 200, and the code behind it.
# ============================================================================


def test_recovery_answers_alike_for_existing_and_absent(cognito_pool, confirmed_user):
    """Recovery always answers 200 with the conditional message, whether or not the account
    is there. The wording is deliberately hypothetical for exactly this reason."""
    label, _ = cognito_pool
    existing_email, _ = confirmed_user

    on_existing = requests.post(_auth_url("password-recovery"),
                                json={"username": existing_email})
    on_absent = requests.post(_auth_url("password-recovery"),
                              json={"username": generate_random_email()})

    assert on_existing.status_code == 200, f"[{label}] existing: {on_existing.text}"
    assert on_absent.status_code == 200, f"[{label}] absent: {on_absent.text}"
    assert _status(on_existing) == "If your account exists, you will receive a code", \
        on_existing.text
    assert on_existing.text == on_absent.text, (
        f"[{label}] recovery must not reveal existence: "
        f"{on_existing.text} vs {on_absent.text}"
    )


def test_recovery_mails_a_code_only_to_a_real_account(cognito_pool, require_mailosaur):
    """The masked 200 hides a real difference in side effect, which is correct: a real
    account gets a code it asked for, an address with no account gets no mail at all.
    Reading both inboxes is the only way to see that the masking is response-only."""
    label, pool_id = cognito_pool
    real_email = generate_mailosaur_email()
    absent_email = generate_mailosaur_email()
    if not real_email or not absent_email or real_email == absent_email:
        pytest.skip("two distinct Mailosaur addresses unavailable")

    cognito = boto3.client("cognito-idp", region_name=REGION)
    cognito.admin_create_user(
        UserPoolId=pool_id, Username=real_email, MessageAction="SUPPRESS",
        UserAttributes=[{"Name": "email", "Value": real_email},
                        {"Name": "email_verified", "Value": "true"}])
    cognito.admin_set_user_password(
        UserPoolId=pool_id, Username=real_email,
        Password=generate_test_password(), Permanent=True)
    try:
        started = time.time()
        assert requests.post(_auth_url("password-recovery"),
                             json={"username": real_email}).status_code == 200
        assert requests.post(_auth_url("password-recovery"),
                             json={"username": absent_email}).status_code == 200

        assert get_verification_code_from_server(
            recipient_email=real_email, since_timestamp=started, **CODE_WAIT), \
            f"[{label}] a real account must receive its recovery code"
        assert get_verification_code_from_server(
            recipient_email=absent_email, since_timestamp=started, **NO_CODE_WAIT) is None, \
            f"[{label}] an address with no account must receive nothing"
    finally:
        try:
            cognito.admin_delete_user(UserPoolId=pool_id, Username=real_email)
        except Exception:  # noqa: BLE001
            pass


def test_recovery_completes_with_the_mailed_code(cognito_pool, require_mailosaur):
    """End to end: the code that arrives resets the password, and the new password signs in.
    This is what makes the confirmation-failure assertions below meaningful."""
    label, pool_id = cognito_pool
    email = generate_mailosaur_email()
    if not email:
        pytest.skip("Mailosaur address unavailable")
    cognito = boto3.client("cognito-idp", region_name=REGION)
    cognito.admin_create_user(
        UserPoolId=pool_id, Username=email, MessageAction="SUPPRESS",
        UserAttributes=[{"Name": "email", "Value": email},
                        {"Name": "email_verified", "Value": "true"}])
    cognito.admin_set_user_password(
        UserPoolId=pool_id, Username=email,
        Password=generate_test_password(), Permanent=True)
    try:
        started = time.time()
        assert requests.post(_auth_url("password-recovery"),
                             json={"username": email}).status_code == 200
        code = get_verification_code_from_server(
            recipient_email=email, since_timestamp=started, **CODE_WAIT)
        assert code, f"[{label}] no recovery code arrived"

        new_password = generate_test_password()
        confirmed = requests.post(_auth_url("password-recovery/confirmation"),
                                  json={"username": email, "code": code,
                                        "new_password": new_password})
        assert confirmed.status_code == 200, f"[{label}] {confirmed.text}"
        assert _status(confirmed) == "Password reset successful", confirmed.text

        signed_in = requests.post(_auth_url("token"),
                                  json={"username": email, "password": new_password})
        assert signed_in.status_code == 200, \
            f"[{label}] the reset password must sign in: {signed_in.text}"
    finally:
        try:
            cognito.admin_delete_user(UserPoolId=pool_id, Username=email)
        except Exception:  # noqa: BLE001
            pass


def test_recovery_confirmation_rejects_existing_and_absent_alike(cognito_pool, confirmed_user):
    """A bad recovery code fails the same way for a real account and for one that is not
    there, so the confirmation step is not an oracle either."""
    label, _ = cognito_pool
    existing_email, _ = confirmed_user
    new_password = generate_test_password()

    on_existing = requests.post(_auth_url("password-recovery/confirmation"),
                                json={"username": existing_email,
                                      "code": "000000",
                                      "new_password": new_password})
    on_absent = requests.post(_auth_url("password-recovery/confirmation"),
                              json={"username": generate_random_email(),
                                    "code": "000000",
                                    "new_password": new_password})

    assert on_existing.status_code == 400, f"[{label}] existing: {on_existing.text}"
    assert on_absent.status_code == 400, f"[{label}] absent: {on_absent.text}"
    assert _status(on_existing) == "Invalid verification code", on_existing.text
    assert on_existing.text == on_absent.text, (
        f"[{label}] confirmation must not reveal existence: "
        f"{on_existing.text} vs {on_absent.text}"
    )


# ============================================================================
# token/refresh, signout, password — the token-bearing endpoints.
# ============================================================================


def test_refresh_rotation_and_reuse_detection(cognito_pool, confirmed_user):
    """Moved from test_user_auth.py and parameterised over both pools.

    Rotation and reuse detection are one behaviour: every redemption spends the presented
    token. Redeeming returns a new token; re-presenting a spent one is refused and takes
    the whole login family with it, which is what makes rotation a theft detector rather
    than just hygiene.

    The original drove the OIDC surface through the User helper as well. Here both
    redemptions go through the legacy endpoint, because that is the surface this suite is
    about and it is the one whose behaviour must not vary by pool.

    Two rotations happen before the replay, and that is load-bearing. refreshtoken.go grants a
    60s rotation grace to a token exactly ONE step behind, because a lost rotation response is
    indistinguishable from reuse and re-issuing is the safe reading. Replaying immediately after a
    single rotation therefore answers 200 by design — that path is test_refresh_grace_tolerates_
    lost_response_retry in test_user_auth.py. Rotating twice puts the original two counters back,
    which is unambiguous theft, and tests it without sleeping out the grace.
    """
    label, _ = cognito_pool
    email, password = confirmed_user

    signin = requests.post(_auth_url("token"),
                           json={"username": email, "password": password})
    assert signin.status_code == 200, f"[{label}] {signin.text}"
    original = signin.json()["refresh_token"]

    rotated = requests.post(_auth_url("token/refresh"), json={"refresh_token": original})
    assert rotated.status_code == 200, f"[{label}] {rotated.text}"
    rotated_body = rotated.json()
    assert rotated_body["access_token"], rotated.text
    assert rotated_body["refresh_token"] != original, \
        f"[{label}] the refresh must rotate the token"

    second = requests.post(_auth_url("token/refresh"),
                           json={"refresh_token": rotated_body["refresh_token"]})
    assert second.status_code == 200, f"[{label}] {second.text}"
    newest = second.json()["refresh_token"]
    assert newest != rotated_body["refresh_token"], \
        f"[{label}] every redemption must rotate"

    # Two steps behind: outside the one-step grace, so this is reuse, and the refusal is uniform.
    replay = requests.post(_auth_url("token/refresh"), json={"refresh_token": original})
    assert replay.status_code == 401, f"[{label}] a spent token must be refused: {replay.text}"
    assert _status(replay) == "Invalid refresh token", replay.text

    # Reuse is theft, so the family dies — the newest token stops working too.
    dead = requests.post(_auth_url("token/refresh"), json={"refresh_token": newest})
    assert dead.status_code == 401, \
        f"[{label}] the family must be gone after reuse: {dead.status_code} {dead.text}"


@pytest.mark.parametrize("case,token", [
    ("never-issued", "nope.nope.nope"),
    ("empty", ""),
])
def test_refresh_rejects_tokens_that_name_no_login(cognito_pool, case, token):
    """A token that was never issued is refused with the same 401 as a spent one, giving no
    reuse oracle and no existence oracle."""
    label, _ = cognito_pool
    refused = requests.post(_auth_url("token/refresh"), json={"refresh_token": token})
    assert refused.status_code == 401, f"[{label}/{case}] {refused.text}"
    assert _status(refused) == "Invalid refresh token", refused.text


def test_signout_revokes_a_live_refresh_token(cognito_pool, confirmed_user):
    """Signout is reported, not silently accepted: it revokes the token it was given, and
    that token then stops refreshing."""
    label, _ = cognito_pool
    email, password = confirmed_user

    signin = requests.post(_auth_url("token"),
                           json={"username": email, "password": password})
    assert signin.status_code == 200, f"[{label}] {signin.text}"
    refresh_token = signin.json()["refresh_token"]

    signed_out = requests.post(_auth_url("signout"), json={"refresh_token": refresh_token})
    assert signed_out.status_code == 200, f"[{label}] {signed_out.text}"
    assert _status(signed_out) == "Successfully signed out", signed_out.text

    after = requests.post(_auth_url("token/refresh"), json={"refresh_token": refresh_token})
    assert after.status_code == 401, \
        f"[{label}] a signed-out token must not refresh: {after.status_code} {after.text}"


def test_signout_requires_a_token_and_refuses_global(cognito_pool):
    """The two request-shape refusals, which are deliberately explicit rather than masked: a
    signout that revoked nothing must say so instead of answering 200."""
    label, _ = cognito_pool

    missing = requests.post(_auth_url("signout"), json={})
    assert missing.status_code == 400, f"[{label}] {missing.text}"
    assert _status(missing) == "Refresh token is required", missing.text

    global_out = requests.post(_auth_url("signout"),
                               json={"refresh_token": "whatever", "global": "true"})
    assert global_out.status_code == 400, f"[{label}] {global_out.text}"
    assert _status(global_out) == (
        "All-device signout is not supported; sign out each session with its refresh token"
    ), global_out.text


def test_password_change_succeeds_and_the_new_password_signs_in(cognito_pool, confirmed_user):
    """A change authenticated by the caller's own access token, verified by signing in with
    the new password rather than by trusting the 200."""
    label, _ = cognito_pool
    email, password = confirmed_user

    signin = requests.post(_auth_url("token"),
                           json={"username": email, "password": password})
    assert signin.status_code == 200, f"[{label}] {signin.text}"
    access_token = signin.json()["access_token"]

    new_password = generate_test_password()
    changed = requests.post(_auth_url("password"),
                            json={"access_token": access_token,
                                  "old_password": password,
                                  "new_password": new_password})
    assert changed.status_code == 200, f"[{label}] {changed.text}"

    assert requests.post(_auth_url("token"),
                         json={"username": email, "password": new_password}).status_code == 200, \
        f"[{label}] the new password must sign in"
    stale = requests.post(_auth_url("token"),
                          json={"username": email, "password": password})
    assert stale.status_code == 401, f"[{label}] the old password must stop working: {stale.text}"


@pytest.mark.parametrize("case", ["wrong-old-password", "absent-user-token", "garbage-token"])
def test_password_change_fails_identically_without_proof(cognito_pool, confirmed_user, case):
    """Every way of failing to prove whose password is being changed gets one 401 and one
    body — a wrong old password, a token for an account that no longer exists, and a token
    that was never ours. The caller's token names the account, so a distinguishable failure
    here would let someone probe accounts by trying to repassword them."""
    label, pool_id = cognito_pool
    email, password = confirmed_user
    cognito = boto3.client("cognito-idp", region_name=REGION)

    if case == "garbage-token":
        access_token = "not.a.token"
        old_password = password
    else:
        signin = requests.post(_auth_url("token"),
                               json={"username": email, "password": password})
        assert signin.status_code == 200, f"[{label}] {signin.text}"
        access_token = signin.json()["access_token"]
        old_password = password if case == "absent-user-token" else generate_test_password()
        if case == "absent-user-token":
            # The token stays valid while the account behind it is gone.
            cognito.admin_delete_user(UserPoolId=pool_id, Username=email)

    refused = requests.post(_auth_url("password"),
                            json={"access_token": access_token,
                                  "old_password": old_password,
                                  "new_password": generate_test_password()})
    assert refused.status_code == 401, f"[{label}/{case}] {refused.text}"
    assert _status(refused) == "Password change failed", refused.text


# ============================================================================
# Request-shape validation, which is not existence-sensitive and so may speak plainly.
# ============================================================================


@pytest.mark.parametrize("payload,expected", [
    ({"password": "Aa1!aaaa"}, "Provide exactly one of email or phone_number"),
    ({"email": "a@b.com", "phone_number": "+10000000000", "password": "Aa1!aaaa"},
     "Provide exactly one of email or phone_number"),
    ({"email": "a@b.com"}, "Password is required"),
])
def test_signup_validates_its_request_before_touching_the_pool(cognito_pool, payload, expected):
    """These 400s name the problem, unlike the existence answers: a malformed request
    reveals nothing about who has an account, and a vague error would only make the API
    harder to use."""
    label, _ = cognito_pool
    refused = requests.post(_auth_url("signup"), json=payload)
    assert refused.status_code == 400, f"[{label}] {refused.text}"
    assert _status(refused) == expected, refused.text
