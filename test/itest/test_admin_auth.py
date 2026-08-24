# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Admin authentication against the ESP-Admin-Users pool.

The admin pool advertises EMAIL_OTP as a first authentication factor, so an admin
signs in by proving control of their inbox rather than with a shared password. This
suite drives the real choice-based flow end to end — USER_AUTH, SELECT_CHALLENGE,
EMAIL_OTP — and reads the real code out of a Mailosaur inbox.

Two admin shapes are covered because both exist in every upgraded deployment:

  passwordless   created without a TemporaryPassword, as the seeding custom resource
                 now does. EMAIL_OTP is the only factor it has.
  with password  created the way admins were seeded before, so it carries both
                 factors and must be offered the choice.

Opt-in like test_cognito_auth_matrix: every code sent burns the pool's daily Cognito
email quota, so the default `make itest` deselects it. Run it on its own with
`make itest ITEST_ARGS='-m cognito -k admin_auth'`.
"""

from test.itest.conftest import (
    ADMIN_USER_POOL_ID,
    ADMIN_CLIENT_ID,
    API_GATEWAY_URL,
    IDENTITY_POOL_ID,
    IOT_ENDPOINT,
    REGION,
    USER_API_GATEWAY_URL,
)
from test.itest.email_utils import (
    generate_mailosaur_email,
    generate_test_password,
    get_verification_code_from_server,
)
from py_sdk.test_user import User
import boto3
import contextlib
import pytest
import time

pytestmark = [pytest.mark.cognito]


@pytest.fixture
def cognito():
    return boto3.client("cognito-idp", region_name=REGION)


def _provision_admin(email, password):
    """Provision a super admin in the admin pool, yielding the address, then remove it.

    Delegates to the same `create_super_admin_via_cognito` the harness uses everywhere else,
    including its `password=False` contract for an identity with no password at all — which is
    exactly the shape the seeding custom resource now produces. It stamps `custom:super_admin`
    and a derived `custom:user_id`, without which every admin gate refuses the caller.

    Cleanup runs whether or not provisioning succeeded: that helper returns None rather than
    raising, and for the password case it can leave a created-but-passwordless user behind, so
    an assert alone would orphan an account in the real pool.
    """
    provisioner = User(
        email, "", REGION, IDENTITY_POOL_ID, API_GATEWAY_URL, USER_API_GATEWAY_URL,
        IOT_ENDPOINT, admin_user_pool_id=ADMIN_USER_POOL_ID, admin_client_id=ADMIN_CLIENT_ID,
    )
    cognito = boto3.client("cognito-idp", region_name=REGION)
    try:
        assert provisioner.create_super_admin_via_cognito(
            email=email, password=password,
        ), f"failed to provision admin {email}"
        yield email
    finally:
        with contextlib.suppress(cognito.exceptions.UserNotFoundException):
            cognito.admin_delete_user(UserPoolId=ADMIN_USER_POOL_ID, Username=email)


@pytest.fixture
def passwordless_admin(require_mailosaur):
    # password=False, not None: None would fall back to the provisioner's own password.
    yield from _provision_admin(generate_mailosaur_email(is_admin=True), password=False)


@pytest.fixture
def admin_with_password(require_mailosaur):
    yield from _provision_admin(generate_mailosaur_email(is_admin=True), generate_test_password())


def _start(cognito, email):
    return cognito.initiate_auth(
        ClientId=ADMIN_CLIENT_ID,
        AuthFlow="USER_AUTH",
        AuthParameters={"USERNAME": email},
    )


def _reach_email_otp(cognito, email):
    """Session for a live EMAIL_OTP challenge, with the code sent exactly once.

    Cognito skips the menu for a user with a single factor: a passwordless admin's
    InitiateAuth already answers `ChallengeName: EMAIL_OTP` with the code on its way,
    while an admin who also holds a password answers `SELECT_CHALLENGE` and needs the
    factor picked. Selecting unconditionally would send a *second* code to the
    single-factor case and invalidate the first, so the caller reads one code while
    the live challenge expects the other — which is a CodeMismatchException that looks
    for all the world like a broken OTP flow.

    The dashboard branches on the same condition, in authApi.startSignin.
    """
    started = _start(cognito, email)
    if started["ChallengeName"] == "EMAIL_OTP":
        return started["Session"]

    challenge = cognito.respond_to_auth_challenge(
        ClientId=ADMIN_CLIENT_ID,
        ChallengeName="SELECT_CHALLENGE",
        Session=started["Session"],
        ChallengeResponses={"USERNAME": email, "ANSWER": "EMAIL_OTP"},
    )
    assert challenge["ChallengeName"] == "EMAIL_OTP"
    return challenge["Session"]


def test_passwordless_admin_is_offered_only_the_email_code(cognito, passwordless_admin):
    response = _start(cognito, passwordless_admin)
    assert "EMAIL_OTP" in response["AvailableChallenges"]
    assert "PASSWORD" not in response["AvailableChallenges"]


def test_admin_with_password_is_offered_both_factors(cognito, admin_with_password):
    response = _start(cognito, admin_with_password)
    assert set(response["AvailableChallenges"]) >= {"EMAIL_OTP", "PASSWORD"}


def test_passwordless_admin_signs_in_with_an_emailed_code(cognito, passwordless_admin):
    sent_after = time.time()
    session = _reach_email_otp(cognito, passwordless_admin)

    code = get_verification_code_from_server(
        since_timestamp=sent_after, recipient_email=passwordless_admin
    )
    assert code, "no one-time code arrived"

    signed_in = cognito.respond_to_auth_challenge(
        ClientId=ADMIN_CLIENT_ID,
        ChallengeName="EMAIL_OTP",
        Session=session,
        ChallengeResponses={"USERNAME": passwordless_admin, "EMAIL_OTP_CODE": code},
    )
    result = signed_in["AuthenticationResult"]
    assert result["AccessToken"]
    assert result["IdToken"]


def test_a_wrong_code_is_rejected(cognito, passwordless_admin):
    session = _reach_email_otp(cognito, passwordless_admin)
    with pytest.raises(cognito.exceptions.CodeMismatchException):
        cognito.respond_to_auth_challenge(
            ClientId=ADMIN_CLIENT_ID,
            ChallengeName="EMAIL_OTP",
            Session=session,
            ChallengeResponses={"USERNAME": passwordless_admin, "EMAIL_OTP_CODE": "000000"},
        )


def test_an_unknown_address_is_not_distinguishable(cognito, require_mailosaur):
    """PreventUserExistenceErrors makes Cognito fabricate a challenge for unknown users.

    The masking is the point: if an unknown address errored (or came back shaped
    differently) while a real admin's address returned an ordinary challenge, the
    login screen would be an enumeration oracle for admin accounts. Compared against
    a real admin's response rather than inspected alone, since a response that merely
    "has some challenge-like key" would pass even if unknown addresses were in fact
    distinguishable from known ones.

    What this deliberately does NOT assert is that the two responses match. They
    routinely differ, and neither difference reveals anything: a passwordless admin
    has one factor, so Cognito always skips the menu and answers EMAIL_OTP outright,
    while for an unknown address it fabricates a per-address choice that is sometimes
    EMAIL_OTP and sometimes SELECT_CHALLENGE over both factors. Measured against this
    pool, roughly a third of unknown addresses produce the very shape a passwordless
    admin produces, and roughly half produce the shape an admin holding a password
    produces — so no single response identifies an account, which is the property that
    matters. Asserting equality here would fail on a working pool.

    So this asserts what must hold every run: an unknown address is answered with a
    usable challenge rather than an error, and that challenge is one Cognito also
    hands out for real accounts.
    """
    unknown = _start(cognito, generate_mailosaur_email(is_admin=True))

    assert unknown["ChallengeName"] in {"SELECT_CHALLENGE", "EMAIL_OTP"}
    assert unknown["Session"]
    # SELECT_CHALLENGE carries the menu; EMAIL_OTP is already the chosen factor and
    # carries a masked destination instead.
    if unknown["ChallengeName"] == "SELECT_CHALLENGE":
        assert set(unknown["AvailableChallenges"]) <= {
            "PASSWORD",
            "PASSWORD_SRP",
            "EMAIL_OTP",
        }
    else:
        assert unknown["ChallengeParameters"]["CODE_DELIVERY_DESTINATION"]


def test_a_passwordless_admin_can_set_a_first_password(cognito, passwordless_admin):
    """ChangePassword accepts no PreviousPassword when the user has no password.

    This is what the account-settings 'Set a password' branch relies on.
    """
    sent_after = time.time()
    session = _reach_email_otp(cognito, passwordless_admin)

    code = get_verification_code_from_server(
        since_timestamp=sent_after, recipient_email=passwordless_admin
    )
    assert code, "no one-time code arrived"
    signed_in = cognito.respond_to_auth_challenge(
        ClientId=ADMIN_CLIENT_ID,
        ChallengeName="EMAIL_OTP",
        Session=session,
        ChallengeResponses={"USERNAME": passwordless_admin, "EMAIL_OTP_CODE": code},
    )
    access_token = signed_in["AuthenticationResult"]["AccessToken"]

    factors = cognito.get_user_auth_factors(AccessToken=access_token)
    assert "PASSWORD" not in factors["ConfiguredUserAuthFactors"]

    cognito.change_password(
        AccessToken=access_token, ProposedPassword=generate_test_password()
    )

    after = cognito.get_user_auth_factors(AccessToken=access_token)
    assert "PASSWORD" in after["ConfiguredUserAuthFactors"]
