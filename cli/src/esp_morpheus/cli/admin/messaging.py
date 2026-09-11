# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus admin ses ...` and `admin sns ...` — the account-level email and SMS setup.

These reach AWS directly rather than through the deployment API, so they need AWS credentials for
the account the outputs name.
"""

import click

from ... import paths
from .. import output
from ..context import pass_session


@click.group('ses')
def ses():
    """Simple Email Service, which delivers the OTP emails."""


def _mailosaur_credentials(session):
    """Mailosaur server ID and API key from the CLI config; fails when either is missing."""
    server_id = session.config.get('mailosaur_server_id')
    api_key = session.config.get('mailosaur_api_key')
    if not server_id or not api_key:
        output.fail('Set mailosaur_server_id and mailosaur_api_key in '
                    f"{paths.test_config_path()} to use --mailosaur.")
    return server_id, api_key


@ses.command('setup-sender')
@click.argument('email', required=False)
@click.option('--mailosaur', 'use_mailosaur', is_flag=True,
              help='Mint a Mailosaur address and follow its verification link automatically.')
@click.option('--wait', 'wait_seconds', type=float, default=300.0, show_default=True,
              help='Seconds to wait for the address to become verified.')
@pass_session
def ses_setup_sender(session, email, use_mailosaur, wait_seconds):
    """Verify the address the OTP emails are sent from, then select it.

    SES mails a verification link to EMAIL, which its owner opens; re-run this afterwards to select
    the address. --mailosaur instead mints a test address and opens the link for you.
    """
    from ...sdk import mailosaur
    from ...sdk import ses as ses_sdk

    if bool(email) == use_mailosaur:
        output.fail('Name the address to verify, or pass --mailosaur to mint one.')

    settings = session.require_aws()
    confirm = None

    if use_mailosaur:
        server_id, api_key = _mailosaur_credentials(session)
        # The local part is scoped by account and region so concurrent accounts each verify and read
        # their own inbox; SES identities are per account and region.
        prefix = session.config.get('otp_ses_sender_prefix', 'ses-sender')
        email = mailosaur.address(f"{prefix}-{settings.account_id}-{settings.region}", server_id)

        def confirm(since):
            return mailosaur.follow_link(server_id, api_key, ses_sdk.VERIFICATION_LINK_PATTERN,
                                         recipient_email=email, since_timestamp=since)

    if ses_sdk.is_verified(email, settings.region):
        output.info(f"{email} is already verified.")
    else:
        if not use_mailosaur:
            output.info(f"SES is mailing a verification link to {email}; open it to finish.")
        if not ses_sdk.ensure_verified(email, settings.region, confirm=confirm,
                                       timeout=wait_seconds):
            output.fail(f"{email} is still unverified. Open the link SES sent, then run this again.")
        output.ok(f"Verified {email}.")

    session.config['otp_ses_sender'] = email
    path = session.write_config()
    output.ok(f"Saved as otp_ses_sender in {path}")

    try:
        ses_sdk.set_active_sender(email, settings.region)
        output.ok(f"Marked {email} as the active global sender.")
    except Exception as e:  # noqa: BLE001 - the identity is verified either way
        output.fail(f"Verified {email} but could not mark it active: {e}")


@ses.command('request-production')
@pass_session
def ses_request_production(session):
    """File the SES production-access request for this region.

    Approval is asynchronous. Idempotent — it skips when the account is already in production.
    """
    import boto3

    region = session.require_aws().region
    client = boto3.client('sesv2', region_name=region)
    try:
        if client.get_account().get('ProductionAccessEnabled'):
            output.info('Already in production; nothing to do.')
            return
    except Exception as e:  # noqa: BLE001
        output.fail(f"Could not read the SES account status: {e}")

    try:
        client.put_account_details(
            MailType='TRANSACTIONAL',
            WebsiteURL=session.config.get('ses_website_url',
                                          'https://rainmaker.espressif.com'),
            UseCaseDescription=session.config.get(
                'ses_use_case_description',
                'Transactional passwordless login one-time codes (OTP) for ESP RainMaker users.'),
            ProductionAccessEnabled=True,
        )
        output.ok(f"Filed the SES production-access request for {region}; approval is asynchronous.")
    except client.exceptions.ConflictException:
        output.info('A production-access request is already pending.')
    except Exception as e:  # noqa: BLE001
        output.fail(f"Failed to request SES production access: {e}")


@click.group('sns')
def sns():
    """Simple Notification Service, which delivers the OTP text messages."""


@sns.command('request-production')
@pass_session
def sns_request_production(session):
    """Leave the SNS SMS sandbox, then raise the monthly spend limit.

    AWS exposes no API for the sandbox-exit case, and the spend limit is capped at $1 until the exit
    lands, so this prints the console link and raises the limit only once already out.
    """
    import boto3

    region = session.require_aws().region
    client = boto3.client('sns', region_name=region)
    try:
        in_sandbox = client.get_sms_sandbox_account_status().get('IsInSandbox', True)
    except Exception as e:  # noqa: BLE001
        output.fail(f"Could not read the SNS SMS sandbox status: {e}")

    if in_sandbox:
        output.warn('In the SMS sandbox. Exit has no API — open the case once in the console:')
        output.plain(f"    https://{region}.console.aws.amazon.com/sns/v3/home"
                     f"?region={region}#/mobile/text-messaging")
        output.plain('    (Text messaging > Account information > Exit SMS sandbox.)')
        output.plain('    Re-run this command after the exit to raise the monthly spend limit.')
        return

    spend_limit = str(session.config.get('sns_sms_spend_limit_usd', 100))
    try:
        client.set_sms_attributes(attributes={'MonthlySpendLimit': spend_limit})
        output.ok(f"Out of the SMS sandbox; set the monthly SMS spend limit to ${spend_limit}.")
    except Exception as e:  # noqa: BLE001
        output.fail(f"Out of the sandbox, but could not set the spend limit to ${spend_limit}: {e}")
