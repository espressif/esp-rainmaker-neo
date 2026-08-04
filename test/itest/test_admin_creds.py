# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Integration tests for the superadmin admin-credentials APIs.

Endpoints under test:
  POST /v1/admin/credentials  (rmng API, SigV4)        -> Lambda concurrency read
  POST /v1/admin/credentials  (ESP User API, Cognito)  -> SES + SMS sandbox reads

Each returns short-lived STS credentials whose session policy is scoped to the account states
the post-deployment page displays. The tests assert the credentials reach a superadmin, are
denied to a non-admin, and — end to end — can perform their own read, cannot perform the other
stack's, and cannot change any setting.
"""
from botocore.exceptions import ClientError

from py_sdk.test_user import user_log
from test.itest.conftest import REGION

import boto3


def _assert_cred_shape(body):
    for field in ("access_key", "secret_key", "session_token", "expiration"):
        assert field in body, f"missing {field} in credentials response: {body}"


def _client(service, creds):
    return boto3.client(
        service,
        region_name=REGION,
        aws_access_key_id=creds["access_key"],
        aws_secret_access_key=creds["secret_key"],
        aws_session_token=creds["session_token"],
    )


def _assert_denied(call, description):
    """An out-of-scope call must be refused by the session policy, not succeed."""
    try:
        call()
    except ClientError as e:
        code = e.response["Error"]["Code"]
        assert code in ("AccessDeniedException", "AccessDenied", "AuthorizationError"), \
            f"expected AccessDenied for {description}, got {e.response['Error']}"
        return
    assert False, f"out-of-scope {description} must be denied"


# ─── rmng stack (Lambda + service quotas) ─────────────────────────────────────

def test_rmng_admin_creds_denied_for_non_admin(test_user1):
    """A non-admin user is denied rmng admin creds with 403."""
    user_log("Verifying a non-admin cannot fetch rmng admin creds...")
    resp = test_user1.admin_get_rmng_creds()
    assert resp.status_code == 403, f"Expected 403, got {resp.status_code}: {resp.text}"


def test_rmng_admin_creds_are_scoped(super_admin_user):
    """The rmng credentials read the Lambda concurrency limit and nothing else: SES and SMS
    belong to the espuser stack, and the limit is reported, never changed."""
    creds = super_admin_user.admin_get_rmng_creds().json()

    settings = _client("lambda", creds).get_account_settings()
    assert "AccountLimit" in settings, "in-scope lambda:GetAccountSettings should succeed"

    _assert_denied(_client("sns", creds).get_sms_sandbox_account_status,
                   "sns sandbox status with rmng creds")
    _assert_denied(_client("sesv2", creds).get_account, "ses:GetAccount with rmng creds")
    # Read-only: raising the limit is an operator action in the console, not the dashboard's.
    _assert_denied(
        lambda: _client("lambda", creds).put_function_concurrency(
            FunctionName="rmng-admin-creds", ReservedConcurrentExecutions=1),
        "lambda:PutFunctionConcurrency with rmng creds")


def test_espuser_admin_creds_denied_for_non_admin(test_user1):
    """An end user is refused espuser admin creds: the route sits behind the admin
    pool's authorizer, which rejects an end-user token before the handler runs."""
    user_log("Verifying an end user cannot fetch espuser admin creds...")
    resp = test_user1.admin_get_espuser_creds()
    assert resp.status_code in (401, 403), \
        f"Expected 401/403, got {resp.status_code}: {resp.text}"


def test_espuser_admin_creds_are_scoped(super_admin_user):
    """The espuser credentials read the two sandbox states the page displays, cannot reach
    Lambda (the rmng stack's), and cannot change either setting."""
    creds = super_admin_user.admin_get_espuser_creds().json()

    account = _client("sesv2", creds).get_account()
    assert "ProductionAccessEnabled" in account, "in-scope ses:GetAccount should succeed"

    sandbox = _client("sns", creds).get_sms_sandbox_account_status()
    assert "IsInSandbox" in sandbox, "in-scope sns sandbox status should succeed"

    _assert_denied(_client("lambda", creds).get_account_settings,
                   "lambda:GetAccountSettings with espuser creds")
    # Read-only: exiting a sandbox or raising a spend limit is an operator action, not ours.
    _assert_denied(
        lambda: _client("sns", creds).set_sms_attributes(attributes={"MonthlySpendLimit": "1"}),
        "sns:SetSMSAttributes with espuser creds")
    _assert_denied(
        lambda: _client("sesv2", creds).put_account_details(
            MailType="TRANSACTIONAL", WebsiteURL="https://example.com",
            UseCaseDescription="probe", ProductionAccessEnabled=True),
        "ses:PutAccountDetails with espuser creds")
