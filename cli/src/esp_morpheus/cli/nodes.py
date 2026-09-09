# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus user <id> node ...` — associate, dissociate and claim nodes."""

import click

from ..sdk.group import Group
from . import output
from .context import Session, pass_user


@click.group('node')
def node():
    """Associate and claim nodes."""


@node.command('assoc')
@click.argument('device_id')
@click.argument('group_id')
@click.pass_context
def assoc(ctx, device_id, group_id):
    """Associate DEVICE_ID with GROUP_ID.

    For a Matter node use `matter initiate` instead.
    """
    session = ctx.find_object(Session)
    device = session.get_node(device_id)
    error = session.user.do_user_node_assoc(device, group_id)
    if error:
        output.fail(f"Association failed: {error}")
    output.ok(f"Associated {device.node_thing_name} with group {group_id}")


@node.command('remove')
@click.argument('device_id')
@click.argument('group_id')
@click.pass_context
def remove(ctx, device_id, group_id):
    """Remove DEVICE_ID from GROUP_ID."""
    session = ctx.find_object(Session)
    device = session.get_node(device_id)
    Group(session.user).remove_node_from_group(group_id, device.node_thing_name)
    output.ok(f"Removed {device.node_thing_name} from group {group_id}")


@node.command('claim')
@click.argument('mac_addr', required=False)
@click.option('--capability', 'capabilities', multiple=True,
              help='Capability to request. Repeat for several.')
@click.option('--out-dir', type=click.Path(file_okay=False), default='.', show_default=True,
              help='Where to write the issued certificate, key and CA.')
@pass_user
def claim(user, mac_addr, capabilities, out_dir):
    """Run an assisted claim for MAC_ADDR, generating one when it is omitted.

    Writes <node_id>.crt, <node_id>.key and <node_id>-ca.crt so a device can use them.
    """
    import os

    try:
        result = user.claim(mac_addr, capabilities=list(capabilities) or None)
    except RuntimeError as e:
        output.fail(f"Claim failed: {e}")

    node_id = result['node_id']
    os.makedirs(out_dir, exist_ok=True)
    written = {}
    for suffix, contents in (('.crt', result['certificate']),
                             ('.key', result['private_key']),
                             ('-ca.crt', result['ca_certificate'])):
        path = os.path.join(out_dir, f"{node_id}{suffix}")
        with open(path, 'w') as handle:
            handle.write(contents)
        written[suffix.lstrip('-.').replace('crt', 'certificate')] = path

    output.ok(f"Claimed node {node_id} (mac {result['mac_addr']})")
    output.emit_kv(None, {'node_id': node_id, 'mac_addr': result['mac_addr'], **written})
