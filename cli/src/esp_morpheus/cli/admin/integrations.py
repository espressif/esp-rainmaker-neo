# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`esp-morpheus admin integrations ...` — the voice-assistant credentials each skill needs."""

import os
import time
import urllib.error

import click

from ...sdk import alexa_smapi_cli as alexa_smapi, gva_setup as gva_gcp
from .. import output
from ..context import Session, pass_session, pass_user
from ..guide import print_alexa_instructions

DEFAULT_ALEXA_CONFIG = 'alexa_skills_config.json'
ALEXA_CONFIG_KEYS = ('lwa_client_id', 'lwa_client_secret', 'vendor_id',
                     'lwa_refresh_token', 'skill_name', 'manufacturer_name')


def _lwa_value(config, field):
    """Read an LWA key, accepting its old smapi_ name from configs written before the rename: field 'client_id' reads lwa_client_id, else smapi_client_id."""
    return config.get(f'lwa_{field}') or config.get(f'smapi_{field}')


def _check_alexa_config(ctx, param, value):
    """Reject a missing config before the command logs in, naming the keys the file needs."""
    del ctx, param
    if not os.path.isfile(value):
        raise click.UsageError(
            f'Alexa config not found: {value}\n'
            f"Pass --config PATH, or write the file with these keys: "
            f"{', '.join(ALEXA_CONFIG_KEYS)}")
    return value


def alexa_config_option(command):
    """Attach the --config option every SMAPI command reads."""
    return click.option(
        '--config', 'config_file', default=DEFAULT_ALEXA_CONFIG, show_default=True,
        type=click.Path(dir_okay=False), callback=_check_alexa_config, is_eager=True,
        help='JSON file with the SMAPI credentials: ' + ', '.join(ALEXA_CONFIG_KEYS) + '.',
    )(command)


@click.group('integrations')
def integrations():
    """Configure the voice-assistant integrations."""


@integrations.group('alexa')
def alexa():
    """Alexa smart-home skill configuration.

    The SMAPI commands (setup-auto, list-skills, delete-skill) take `--config FILE`, which defaults
    to alexa_skills_config.json. The setup command takes its own file as an argument.
    """


def _alexa_config_to_env(config_file):
    """Populate the env the SMAPI diagnostics read, so list-skills and delete-skill share setup-auto's config file."""
    config = output.read_json_file(config_file, 'Alexa config')
    env_map = {
        'SMAPI_CLIENT_ID': _lwa_value(config, 'client_id'),
        'SMAPI_CLIENT_SECRET': _lwa_value(config, 'client_secret'),
        'SMAPI_VENDOR_ID': config.get('vendor_id'),
        'SMAPI_REFRESH_TOKEN': _lwa_value(config, 'refresh_token'),
    }
    for env_key, value in env_map.items():
        if value:
            os.environ[env_key] = str(value)


SETUP_POLL_SECONDS = 5
# The worker is a Lambda capped at 900s, so waiting any less can report a timeout on a run that is still going.
SETUP_TIMEOUT_SECONDS = 960


def _wait_for_setup(session, integration, ref_id):
    """Block until the backend reports the setup of ref_id finished, printing each stage.

    Shaped like gva_setup.enable_homegraph: a deadline, a fixed interval, and a printed state,
    because an operator watching a terminal needs to see it is alive. Polls the status the
    backend publishes rather than waiting on a value to appear, so a worker that dies reports
    FAILED instead of hanging until the timeout. A status for another ref_id is a previous run's,
    so it is skipped rather than read as this run finishing.
    """
    getter = {'alexa': session.user.alexa_get_configuration}[integration]
    deadline = time.monotonic() + SETUP_TIMEOUT_SECONDS
    seen = None
    while time.monotonic() < deadline:
        response = getter()
        body = response.json() if response.ok else {}
        parsed = body.get('config_status') if isinstance(body, dict) else None
        if parsed and parsed.get('ref_id') == ref_id:
            if parsed.get('stage') and parsed['stage'] != seen:
                seen = parsed['stage']
                output.plain(f"  {seen}")
            if parsed['status'] in ('DONE', 'FAILED'):
                return parsed
        time.sleep(SETUP_POLL_SECONDS)
    output.fail(f"Timed out after {SETUP_TIMEOUT_SECONDS}s waiting for {integration} setup")


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
        client_secret=config['alexa_client_secret'], skill_id=config['skill_id'],
        manufacturer_name=config.get('manufacturer_name')),
        'Alexa configuration stored')
    print_alexa_instructions(session.settings)


@alexa.command('setup-auto')
@alexa_config_option
@click.argument('skill_name', nargs=-1)
@click.pass_context
def alexa_auto(ctx, config_file, skill_name):
    """Reconfigure the deployment's live skill (or create one), then store its credentials.

    The backend runs the SMAPI pipeline, the same one the dashboard uses.
    """
    session = ctx.find_object(Session)
    config = output.read_json_file(config_file, 'Alexa config')
    client_id, client_secret = _lwa_value(config, 'client_id'), _lwa_value(config, 'client_secret')
    refresh_token = _lwa_value(config, 'refresh_token')
    # No keys means the backend's connected Amazon account; a refresh token alone, or half the profile, is a mistake.
    if client_id or client_secret or refresh_token:
        missing = [k for k, v in (('lwa_client_id', client_id), ('lwa_client_secret', client_secret)) if not v]
        if missing:
            output.fail(f"{config_file} is missing {', '.join(missing)}")

    name = ' '.join(skill_name) or config.get('skill_name')
    if not name:
        output.fail('Pass a skill name, or set skill_name in ' + config_file)

    if client_id and not refresh_token:
        try:
            refresh_token = alexa_smapi.get_refresh_token(
                client_id, client_secret, int(os.environ.get('SMAPI_REDIRECT_PORT', '9090')))
        except (RuntimeError, urllib.error.URLError) as e:
            output.fail(f'Amazon consent failed: {e}')

    if client_id:
        output.emit_response(session.user.alexa_put_lwa_connection(
            client_id, client_secret, lwa_refresh_token=refresh_token), show_body=False)
    body = output.emit_response(session.user.alexa_create_auto_configuration(
        skill_name=name, manufacturer_name=config.get('manufacturer_name')), show_body=False)
    ref_id = body['skill_id']
    output.ok(f"Skill {ref_id}; configuring")

    final = _wait_for_setup(session, 'alexa', ref_id)
    if final and final.get('status') == 'FAILED':
        output.fail(f"Configuration failed: {final.get('error') or 'see the rmng-alexa-auto-cfg logs'}")
    output.ok(f"Alexa configuration stored for skill {ref_id}")
    # Not print_alexa_instructions: that is the manual console procedure this command just performed.
    output.plain('')
    output.plain(f'Next: enable "{name}" in the Alexa app (More -> Skills & Games -> Your Skills -> Dev) and link your RainMaker account.')


@alexa.command('list-skills')
@alexa_config_option
@pass_session
def alexa_list(session, config_file):
    """List every Alexa skill under the vendor."""
    del session
    _alexa_config_to_env(config_file)
    alexa_smapi.list_all()


@alexa.command('delete-skill')
@click.argument('skill_id')
@alexa_config_option
@pass_session
def alexa_delete(session, skill_id, config_file):
    """Delete the Alexa skill SKILL_ID."""
    del session
    _alexa_config_to_env(config_file)
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


@gva.command('setup-auto')
@click.argument('project_id')
@click.argument('service_account_name', required=False)
@pass_user
def gva_auto(user, project_id, service_account_name):
    """Enable the HomeGraph API, create the report-state service account and key on PROJECT_ID,
    then store the key.

    Replaces the console procedure in the spec (enable API, create account, grant the OpenID
    Connect Identity Token Creator role, download a key, re-upload it). The Google-side work runs
    locally against your `gcloud` credentials; only the config POST goes through this admin's
    session, so no separate AWS credentials are needed.

    The Google Home Developer Console part (creating the Home project and the cloud-to-cloud
    integration) has no public API and stays manual.
    """
    def post_config(service_account_json):
        output.emit_response(user.gva_post_configuration(service_account_json),
                             'Google Home configuration stored')

    gva_gcp.setup(post_config, project_id, service_account_name)


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
