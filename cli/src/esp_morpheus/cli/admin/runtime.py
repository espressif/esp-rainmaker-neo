# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus admin iot-event-mode ...` and `admin claiming ...` — runtime configuration."""

import click

from .. import output
from ..context import pass_user


@click.group('iot-event-mode')
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


@click.group('claiming')
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
