# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus user <admin> admin ...` — the deployment-wide operations.

Hidden from an end user's help: every command here needs the admin pool, and the group is listed
only when the resolved identity is an admin. Nothing gates on imports, so the same wheel serves
both audiences.
"""

import json
import os

import click

from .. import paths
from ..sdk import alexa_setup as alexa_smapi
from . import output
from .context import Session, pass_session, pass_user
from .guide import print_alexa_instructions

DEFAULT_ALEXA_CONFIG = 'alexa_skills_config.json'


class AdminGroup(click.Group):
    """A group that hides itself from help unless the resolved identity is an admin."""

    def get_short_help_str(self, limit=45):
        return super().get_short_help_str(limit)

    @property
    def hidden(self):
        ctx = click.get_current_context(silent=True)
        session = ctx.find_object(Session) if ctx else None
        return not (session and session.selected_user_is_admin)

    @hidden.setter
    def hidden(self, value):
        pass


@click.group('admin', cls=AdminGroup)
def admin():
    """Deployment-wide administration."""


# --- voice-assistant integrations -------------------------------------------

@admin.group('integrations')
def integrations():
    """Configure the voice-assistant integrations."""


@integrations.group('alexa')
def alexa():
    """Alexa smart-home skill configuration."""


def _alexa_config_to_env(session, config_file):
    """Populate the env the SMAPI helper reads, so one config file drives both halves.

    Example alexa_skills_config.json:
        {"smapi_client_id": "...", "smapi_client_secret": "...",
         "skill_name": "ESP RainMaker", "skill_id": "amzn1.ask.skill.xxxx"}
    """
    config = output.read_json_file(config_file, 'Alexa config')
    env_map = {
        'SMAPI_CLIENT_ID': 'smapi_client_id',
        'SMAPI_CLIENT_SECRET': 'smapi_client_secret',
        'SMAPI_VENDOR_ID': 'vendor_id',
        'SMAPI_REFRESH_TOKEN': 'smapi_refresh_token',
        'ALEXA_REDIRECT_URLS': 'redirect_urls',
    }
    for env_key, config_key in env_map.items():
        value = config.get(config_key)
        if value:
            os.environ[env_key] = ','.join(value) if isinstance(value, list) else str(value)
    # alexa_setup reads outputs itself, keyed off RMNG_OUTPUTS; point it at the source morpheus
    # resolved so --client-outputs applies there too.
    os.environ['RMNG_OUTPUTS'] = session.settings.source
    return config


@alexa.command('setup')
@click.argument('config_file', type=click.Path(exists=True, dir_okay=False))
@click.pass_context
def alexa_config(ctx, config_file):
    """Store the Alexa skill credentials from CONFIG_FILE in the backend."""
    session = ctx.find_object(Session)
    config = output.read_json_file(config_file, 'Alexa config')
    missing = [k for k in ('redirect_urls', 'alexa_client_id', 'alexa_client_secret', 'skill_id')
               if not config.get(k)]
    if missing:
        output.fail(f"{config_file} is missing {', '.join(missing)}")

    output.emit_response(session.user.alexa_post_configuration(
        redirect_uris=config['redirect_urls'], client_id=config['alexa_client_id'],
        client_secret=config['alexa_client_secret'], skill_id=config['skill_id']),
        'Alexa configuration stored')
    print_alexa_instructions(session.settings)


@alexa.command('setup-auto')
@click.option('--config', 'config_file', default=DEFAULT_ALEXA_CONFIG, show_default=True,
              type=click.Path(exists=True, dir_okay=False))
@click.argument('skill_name', nargs=-1)
@click.pass_context
def alexa_auto(ctx, config_file, skill_name):
    """Create or update the skill over SMAPI, then store its configuration.

    The config POST goes through this admin's session, so no separate AWS credentials are needed.
    """
    session = ctx.find_object(Session)
    config = _alexa_config_to_env(session, config_file)

    def post_config(skill_id, client_id, client_secret, redirect_uris):
        output.emit_response(session.user.alexa_post_configuration(
            redirect_uris=redirect_uris, client_id=client_id,
            client_secret=client_secret, skill_id=skill_id), 'Alexa configuration stored')

    alexa_smapi.setup(post_config, skill_id=config.get('skill_id'),
                      skill_name=' '.join(skill_name) or config.get('skill_name'))


@alexa.command('list-skills')
@click.option('--config', 'config_file', default=DEFAULT_ALEXA_CONFIG, show_default=True,
              type=click.Path(exists=True, dir_okay=False))
@pass_session
def alexa_list(session, config_file):
    """List every Alexa skill under the vendor."""
    _alexa_config_to_env(session, config_file)
    alexa_smapi.list_all()


@alexa.command('delete-skill')
@click.argument('skill_id')
@click.option('--config', 'config_file', default=DEFAULT_ALEXA_CONFIG, show_default=True,
              type=click.Path(exists=True, dir_okay=False))
@pass_session
def alexa_delete(session, skill_id, config_file):
    """Delete the Alexa skill SKILL_ID."""
    _alexa_config_to_env(session, config_file)
    alexa_smapi.delete(skill_id)


@integrations.group('gva')
def gva():
    """Google Home integration."""


@gva.command('setup')
@click.argument('service_account_json', type=click.Path(exists=True, dir_okay=False))
@click.pass_context
def gva_setup(ctx, service_account_json):
    """Upload the HomeGraph service-account key in SERVICE_ACCOUNT_JSON."""
    session = ctx.find_object(Session)
    config = output.read_json_file(service_account_json, 'service account')
    output.emit_response(session.user.gva_post_configuration(config),
                         'Google Home configuration stored')


@integrations.group('smartthings')
def smartthings():
    """SmartThings Schema App integration."""


@smartthings.command('setup')
@click.argument('config_file', type=click.Path(exists=True, dir_okay=False))
@pass_user
def st_setup(user, config_file):
    """Store the credentials SmartThings issued, read from CONFIG_FILE.

    Read from a file rather than argv: the client secret is 512 characters, and a secret in argv is
    visible to other processes and lands in shell history.
    """
    config = output.read_json_file(config_file, 'SmartThings config')
    missing = [field for field in ('client_id', 'client_secret') if not config.get(field)]
    if missing:
        output.fail(f"{config_file} is missing {', '.join(missing)}")
    output.emit_response(user.st_post_configuration(config['client_id'], config['client_secret']),
                         'SmartThings configuration stored')


@smartthings.command('get-config')
@pass_user
def st_get(user):
    """Show the stored SmartThings configuration."""
    output.emit_response(user.st_get_configuration())


@smartthings.command('delete-config')
@pass_user
def st_delete(user):
    """Delete the stored SmartThings configuration."""
    output.emit_response(user.st_delete_configuration(), 'SmartThings configuration deleted')


# --- mobile platforms -------------------------------------------------------

@admin.group('platforms')
def platforms():
    """Register the APNS and FCM platform applications."""


def _read_text(path, what):
    try:
        with open(path, 'r') as handle:
            return handle.read()
    except OSError as e:
        output.fail(f"Could not read the {what}: {e}")


_IOS_ARGS = [
    click.argument('p8_key_file', type=click.Path(exists=True, dir_okay=False)),
    click.argument('key_id'),
    click.argument('team_id'),
    click.argument('bundle_id'),
    click.option('--sandbox', is_flag=True, help='Register against the APNS sandbox.'),
]


def _ios_options(f):
    for decorator in reversed(_IOS_ARGS):
        f = decorator(f)
    return f


@platforms.command('register-ios')
@_ios_options
@pass_user
def register_ios(user, p8_key_file, key_id, team_id, bundle_id, sandbox):
    """Register the APNS platform application."""
    if not user.register_ios_platform(_read_text(p8_key_file, 'P8 key'), key_id, team_id,
                                      bundle_id, sandbox):
        output.fail('Failed to register the iOS platform')
    output.ok(f"Registered the iOS platform ({'sandbox' if sandbox else 'production'})")


@platforms.command('update-ios')
@_ios_options
@pass_user
def update_ios(user, p8_key_file, key_id, team_id, bundle_id, sandbox):
    """Update the APNS platform application."""
    if not user.update_mobile_platform(platform='APNS',
                                       authentication_key=_read_text(p8_key_file, 'P8 key'),
                                       key_id=key_id, team_id=team_id, bundle_id=bundle_id,
                                       apns_sandbox=sandbox):
        output.fail('Failed to update the iOS platform')
    output.ok(f"Updated the iOS platform ({'sandbox' if sandbox else 'production'})")


def _android_key(path):
    content = _read_text(path, 'service account JSON')
    try:
        json.loads(content)
    except json.JSONDecodeError as e:
        output.fail(f"Invalid JSON in {path}: {e}")
    return content


@platforms.command('register-android')
@click.argument('service_account_json', type=click.Path(exists=True, dir_okay=False))
@pass_user
def register_android(user, service_account_json):
    """Register the FCM platform application."""
    if not user.register_android_platform(_android_key(service_account_json)):
        output.fail('Failed to register the Android platform')
    output.ok('Registered the Android platform')


@platforms.command('update-android')
@click.argument('service_account_json', type=click.Path(exists=True, dir_okay=False))
@pass_user
def update_android(user, service_account_json):
    """Update the FCM platform application."""
    if not user.update_mobile_platform(platform='GCM',
                                       api_key=_android_key(service_account_json)):
        output.fail('Failed to update the Android platform')
    output.ok('Updated the Android platform')


@platforms.command('list')
@pass_user
def list_platforms(user):
    """List the registered platform applications."""
    # The old CLI called this and never looked at the result, so a failure read as a success.
    result = user.list_mobile_platforms()
    if result is None:
        output.fail('Failed to list the mobile platforms')
    output.emit_table(['integration_id', 'integration_type'],
                      [[entry.get('integration_id'), entry.get('integration_type')]
                       for entry in result.get('integrations', [])])


@platforms.command('delete')
@click.argument('platform_name')
@click.argument('platform_app_name')
@pass_user
def delete_platform(user, platform_name, platform_app_name):
    """Delete platform application PLATFORM_APP_NAME on PLATFORM_NAME."""
    if not user.delete_mobile_platform(platform_name, platform_app_name):
        output.fail('Failed to delete the mobile platform')
    output.ok(f"Deleted mobile platform {platform_app_name}")


# --- node registration ------------------------------------------------------

@admin.group('nodes')
def nodes():
    """Register nodes into the deployment."""


def _tags(value):
    """`key:value` pairs, comma separated. An entry without a colon is not a tag."""
    return [tag.strip() for tag in (value or '').split(',') if ':' in tag]


def _names(value):
    return [name.strip() for name in (value or '').split(',') if name.strip()]


@nodes.command('register')
@click.argument('node_id')
@click.option('--admin-groups', help='Comma-separated admin group names.')
@click.option('--tags', help='Comma-separated key:value tags.')
@click.pass_context
def register_node(ctx, node_id, admin_groups, tags):
    """Register NODE_ID, taken from test_config.json by index or thing name."""
    session = ctx.find_object(Session)
    device = session.get_node(node_id)
    group_names = _names(admin_groups)
    configured = session.config.get('admin_group_name')
    if configured:
        group_names.append(configured)
    if not session.user.register_node(device, _tags(tags), group_names):
        output.fail(f"Failed to register node {device.node_thing_name}")
    output.ok(f"Registered node {device.node_thing_name}")


@nodes.command('bulk-register')
@click.argument('csv_file', type=click.Path(exists=True, dir_okay=False))
@click.option('--admin-groups', help='Comma-separated admin group names.')
@click.option('--tags', help='Comma-separated key:value tags.')
@pass_user
def bulk_register(user, csv_file, admin_groups, tags):
    """Register every node in CSV_FILE."""
    success, s3_path = user.upload_file(csv_file, 'node_cert')
    if not success:
        # The old CLI printed the failure and then submitted the error message as the S3 path.
        output.fail(f"Upload failed: {s3_path}")
    output.ok(f"Uploaded {csv_file} to {s3_path}")
    result = user.bulk_register_nodes(s3_path, _names(admin_groups), _tags(tags))
    if result is None:
        output.fail('Bulk registration failed')
    output.emit_json(result)


@nodes.command('bulk-status')
@click.argument('request_id')
@pass_user
def bulk_status(user, request_id):
    """Show the status of bulk registration REQUEST_ID."""
    result = user.get_bulk_register_status(request_id)
    if result is None:
        output.fail(f"Failed to get the status of request {request_id}")
    output.emit_json(result)


# --- runtime configuration --------------------------------------------------

@admin.group('iot-event-mode')
def iot_event_mode():
    """Read and set the IoT rule action mode."""


def _emit_mode(result, action):
    if isinstance(result, dict):
        output.emit_json(result)
        return
    status = getattr(result, 'status_code', 'unknown')
    detail = getattr(result, 'text', '')
    output.fail(f"Failed to {action} iot-event-mode (status {status}){f': {detail}' if detail else ''}")


@iot_event_mode.command('get')
@pass_user
def get_event_mode(user):
    """Show the mode of node_offline_rule and device_to_cloud_rule."""
    _emit_mode(user.admin_get_iot_event_mode(), 'get')


@iot_event_mode.command('set')
@click.argument('mode', type=click.Choice(['direct', 'sqs']))
@pass_user
def set_event_mode(user, mode):
    """Set both rules to MODE. They switch together."""
    _emit_mode(user.admin_put_iot_event_mode(mode), 'set')


@admin.group('claiming')
def claiming():
    """Assisted claiming."""


@claiming.command('enable')
@click.option('--config', 'config_file', type=click.Path(exists=True, dir_okay=False),
              help='Claiming configuration JSON. Every field is optional.')
@pass_user
def enable_claiming(user, config_file):
    """Store the claiming configuration and mint the CA.

    The claim stacks stand up the CA key and API but leave claiming off: it is on only once a mode
    is configured and the CA is minted. Idempotent — re-running reports the existing CA unchanged.
    """
    config = output.read_json_file(config_file, 'claiming config') if config_file else {}
    # Enabling means the runtime must have a mode, so default it.
    config.setdefault('mode', 'user_authenticated')

    output.emit_response(user.claim_admin_set_config(config),
                         f"Claiming configuration stored (mode={config['mode']})")

    response = user.claim_admin_mint_ca()
    if response.status_code not in (200, 201):
        output.fail(f"Failed to mint the claiming CA (status {response.status_code}): "
                    f"{response.text}")
    body = response.json()
    if response.status_code == 201:
        output.ok(f"Minted the claiming CA (CN={body.get('common_name')}). Claiming is enabled.")
    else:
        output.ok('The claiming CA is already present; claiming is already enabled.')
    if body.get('ca_certificate'):
        output.plain(body['ca_certificate'])


# --- account-level email and SMS --------------------------------------------

@admin.group('ses')
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
    from ..sdk import mailosaur
    from ..sdk import ses as ses_sdk

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


@admin.group('sns')
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
