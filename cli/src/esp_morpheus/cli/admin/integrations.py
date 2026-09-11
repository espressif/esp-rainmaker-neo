# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus admin integrations ...` — the voice-assistant credentials each skill needs."""

import os

import click

from ...sdk import alexa_setup as alexa_smapi
from .. import output
from ..context import Session, pass_session, pass_user
from ..guide import print_alexa_instructions

DEFAULT_ALEXA_CONFIG = 'alexa_skills_config.json'
ALEXA_CONFIG_KEYS = ('smapi_client_id', 'smapi_client_secret', 'vendor_id',
                     'smapi_refresh_token', 'redirect_urls', 'skill_name', 'skill_id')


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
@alexa_config_option
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
@alexa_config_option
@pass_session
def alexa_list(session, config_file):
    """List every Alexa skill under the vendor."""
    _alexa_config_to_env(session, config_file)
    alexa_smapi.list_all()


@alexa.command('delete-skill')
@click.argument('skill_id')
@alexa_config_option
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
