# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus device <node> ...` — the raw node side of the protocol."""

import click

from . import output
from .context import pass_device


@click.command()
@pass_device
def connect(device):
    """Connect the node and subscribe to its from_cloud topic."""
    if not device.connect():
        output.fail('Failed to connect the device or subscribe to from_cloud')
    output.ok('Connected and subscribed to from_cloud')


@click.command('shadow-connect')
@click.argument('shadow_name')
@pass_device
def shadow_connect(device, shadow_name):
    """Connect the node against SHADOW_NAME."""
    if not device.shadow_connect([shadow_name]):
        output.fail(f"Failed to connect to shadow {shadow_name}")
    output.ok(f"Connected to shadow {shadow_name}")


@click.command()
@click.option('--shadow', 'shadow_name', help='Named shadow to subscribe to.')
@click.option('--topic', help='Raw topic to subscribe to.')
@pass_device
def subscribe(device, shadow_name, topic):
    """Subscribe to a named shadow or a raw topic."""
    if bool(shadow_name) == bool(topic):
        raise click.UsageError('give exactly one of --shadow or --topic')
    what = f"named shadow {shadow_name}" if shadow_name else f"topic {topic}"
    subscribed = (device.subscribe(shadow_name=shadow_name) if shadow_name
                  else device.subscribe(topic=topic))
    if not subscribed:
        output.fail(f"Failed to subscribe to {what}")
    output.ok(f"Subscribed to {what}")


@click.command()
@click.argument('shadow_name')
@click.argument('data', nargs=-1)
@click.option('--file', 'file_path', type=click.Path(exists=True, dir_okay=False),
              help='Read the JSON payload from a file.')
@pass_device
def publish(device, shadow_name, data, file_path):
    """Publish DATA to the node's SHADOW_NAME shadow."""
    payload = output.parse_json_arg(' '.join(data), file_path)
    if not device.update_shadow(payload, shadow_name):
        output.fail(f"Failed to publish to shadow {shadow_name}")
    output.ok(f"Published to shadow {shadow_name}")


@click.command('to-cloud')
@click.argument('data', nargs=-1)
@click.option('--file', 'file_path', type=click.Path(exists=True, dir_okay=False),
              help='Read the JSON payload from a file.')
@pass_device
def to_cloud(device, data, file_path):
    """Publish DATA on the node's to_cloud topic."""
    payload = output.parse_json_arg(' '.join(data), file_path)
    if not device.publish_to_cloud(payload):
        output.fail('Failed to publish to the cloud')
    output.ok('Published to the cloud')


@click.command('direct-notify')
@click.argument('data', nargs=-1)
@click.option('--file', 'file_path', type=click.Path(exists=True, dir_okay=False),
              help='Read the JSON payload from a file.')
@pass_device
def direct_notify(device, data, file_path):
    """Send DATA as a direct notification, using the node's cached group info.

    Run `group-info` first, or the node has no group to notify.
    """
    payload = output.parse_json_arg(' '.join(data), file_path)
    if not device.send_direct_notification(payload):
        output.fail('Failed to send the direct notification')
    output.ok('Sent the direct notification')


@click.command('group-info')
@pass_device
def group_info(device):
    """Fetch and print the node's group and subgroups."""
    device.get_group_info()
    if not device.group_id:
        output.fail('The node is not associated with any group')
    fields = {'group_id': device.group_id}
    # A node correctly in a group with no subgroups is a success; the old CLI nested this else
    # under the subgroup check and called it a failure.
    subgroups = getattr(device, 'subgroup_ids', None)
    if subgroups:
        fields['subgroup_ids'] = list(subgroups)
    output.emit_kv('Group info', fields)


@click.command('set-node-config')
@click.argument('config_file', type=click.Path(exists=True, dir_okay=False))
@pass_device
def set_node_config(device, config_file):
    """Publish the node configuration in CONFIG_FILE."""
    if not device.set_node_config(output.read_json_file(config_file, 'node config')):
        output.fail('Failed to set the node configuration')
    output.ok('Set the node configuration')


COMMANDS = [connect, shadow_connect, subscribe, publish, to_cloud, direct_notify, group_info,
            set_node_config]
