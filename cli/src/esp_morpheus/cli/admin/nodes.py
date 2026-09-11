# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus admin nodes ...` — registering nodes into the deployment."""

import click

from .. import output
from ..context import Session, pass_user


@click.group('nodes')
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
