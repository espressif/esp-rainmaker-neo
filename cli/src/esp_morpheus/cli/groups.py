# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus user <id> group ...` — groups, subgroups and group sharing."""

import click

from ..sdk.group import Group
from . import output
from .context import pass_user

GROUP_ID_LABEL = 'group_id'

MATTER_FIELDS = (
    ('fabric_id', 'Fabric ID'),
    ('ipk', 'IPK'),
    ('group_cat_id_admin', 'CAT ID Admin'),
    ('group_cat_id_operate', 'CAT ID Operate'),
    ('root_ca', 'Root CA'),
)

# One column for every group block, so a plain group and a Matter one line up in the same listing.
GROUP_LABEL_WIDTH = max(len(GROUP_ID_LABEL), *(len(label) for _, label in MATTER_FIELDS))


def matter_fields(matter):
    """The Matter fabric as display fields, which the old CLI printed from three separate copies."""
    return {label: matter.get(key, 'N/A') for key, label in MATTER_FIELDS}


def emit_group(group=None, **overrides):
    """One group as a single block.

    The fabric's fields belong to the group, so they share its key column rather than arriving under
    a heading of their own — a heading sat at column zero and broke the block in two. The marker
    rides on the id line, and stays out of the payload so `--raw` still reports a usable group_id.
    """
    group = {**(group or {}), **overrides}
    if output.structured():
        output.emit_json(group)
        return

    group_id = group.get('group_id', '')
    matter = group.get('matter')
    fields = {GROUP_ID_LABEL: f"{group_id} (Matter enabled)" if matter else group_id}
    if group.get('group_name'):
        fields['name'] = group['group_name']
    if group.get('access_type'):
        fields['access'] = group['access_type']
    if group.get('node_ids'):
        fields['nodes'] = group['node_ids']
    subgroups = group.get('subgroups') or []
    if subgroups:
        # Every other subgroup command takes a subgroup_id, so the id travels with the name. The
        # key stays fixed, or one group's block would set a different column from the next.
        fields['subgroups'] = [f"{s.get('subgroup_name', '')} ({s.get('subgroup_id', '')})"
                               for s in subgroups]
    if matter:
        fields.update(matter_fields(matter))
    output.emit_kv(None, fields, min_width=GROUP_LABEL_WIDTH)


def split_csv(ctx, param, value):
    """A comma-separated option value as a list, ignoring empty entries."""
    if not value:
        return []
    return [item.strip() for item in value.split(',') if item.strip()]


@click.group('group')
def group_cmd():
    """Create and manage groups."""


@group_cmd.command('create')
@click.option('--matter', is_flag=True, help='Create the group as a Matter fabric.')
@click.argument('name', nargs=-1, required=True)
@pass_user
def create(user, matter, name):
    """Create a group called NAME."""
    group_name = ' '.join(name)
    api = Group(user)
    if not matter:
        group_id = api.create_group(group_name)
        if not group_id:
            output.fail(f"Failed to create group '{group_name}'")
        user.add_group_id(group_id)
        output.ok(f"Created group '{group_name}'")
        emit_group(group_id=group_id, group_name=group_name)
        return

    try:
        result = api.create_matter_group(group_name)
    except AssertionError as e:
        output.fail(f"Failed to create Matter group '{group_name}': {e}")
    group_id = result['group_id']
    user.add_group_id(group_id)
    output.ok(f"Created Matter group '{group_name}'")
    emit_group(group_id=group_id, group_name=group_name, matter=result.get('matter', {}))


@group_cmd.command('list')
@pass_user
def list_groups(user):
    """List the groups this user can see."""
    data = Group(user).list_groups()
    groups = data.get('groups') or []
    if not groups:
        output.info('No groups found for this user.')
        return

    if output.structured():
        output.emit_json(groups)
    for position, entry in enumerate(groups):
        if not output.structured():
            if position:
                output.plain('')
            emit_group(entry)
        user.add_group_id(entry['group_id'])
    output.info(f"Stored {len(user.get_group_ids())} group(s) for {user.username}")


@group_cmd.command('rename')
@click.argument('group_id')
@click.argument('name', nargs=-1, required=True)
@pass_user
def rename(user, group_id, name):
    """Rename GROUP_ID to NAME."""
    new_name = ' '.join(name)
    Group(user).update_group(group_id, new_name)
    output.ok(f"Renamed group {group_id} to '{new_name}'")


@group_cmd.command('add-capabilities')
@click.argument('group_id')
@click.argument('capabilities', callback=split_csv)
@pass_user
def add_capabilities(user, group_id, capabilities):
    """Enable CAPABILITIES (comma-separated) on GROUP_ID.

    Passing `matter` converts the group into a Matter fabric.
    """
    if not capabilities:
        raise click.BadParameter('give at least one capability, e.g. matter')
    response = Group(user).add_group_capabilities(group_id, capabilities)
    curated = not output.structured()
    body = output.emit_response(response, f"Enabled {', '.join(capabilities)} on {group_id}",
                                show_body=not curated)
    if curated:
        emit_group(group_id=group_id, matter=(body or {}).get('matter'))


@group_cmd.command('share')
@click.argument('group_id')
@click.argument('access_type')
@click.option('--username', help='Invitee. Omit for a QR-code share, whose request id is the '
                                 'payload the client encodes.')
@pass_user
def share(user, group_id, access_type, username):
    """Share GROUP_ID with ACCESS_TYPE."""
    api = Group(user)
    try:
        if username:
            response = api.share_group(group_id, username, access_type)
            request_id = response.json().get('request_id')
            output.ok(f"Shared group {group_id} with {username} ({access_type})")
        else:
            request_id = api.share_group_by_qr_code(group_id, access_type)
            output.ok(f"Created a QR-code sharing request for group {group_id} ({access_type})")
    except AssertionError as e:
        output.fail(f"Failed to share group {group_id}: {e}")
    output.emit_kv(None, {'request_id': request_id})


# --- subgroups --------------------------------------------------------------

@group_cmd.group('subgroup')
def subgroup():
    """Create and manage subgroups."""


@subgroup.command('create')
@click.argument('group_id')
@click.argument('name', nargs=-1, required=True)
@pass_user
def subgroup_create(user, group_id, name):
    """Create a subgroup called NAME inside GROUP_ID."""
    subgroup_name = ' '.join(name)
    subgroup_id = Group(user).create_subgroup(group_id, subgroup_name)
    if not subgroup_id:
        output.fail(f"Failed to create subgroup '{subgroup_name}' in group {group_id}")
    output.ok(f"Created subgroup '{subgroup_name}'")
    output.emit_kv(None, {'subgroup_id': subgroup_id})


@subgroup.command('rename')
@click.argument('group_id')
@click.argument('subgroup_id')
@click.argument('name', nargs=-1, required=True)
@pass_user
def subgroup_rename(user, group_id, subgroup_id, name):
    """Rename SUBGROUP_ID to NAME."""
    new_name = ' '.join(name)
    Group(user).update_subgroup(group_id, subgroup_id, new_name)
    output.ok(f"Renamed subgroup {subgroup_id} to '{new_name}'")


@subgroup.command('add-node')
@click.argument('group_id')
@click.argument('subgroup_id')
@click.argument('node_id')
@pass_user
def subgroup_add_node(user, group_id, subgroup_id, node_id):
    """Add NODE_ID to SUBGROUP_ID."""
    Group(user).add_node_to_subgroup(group_id, subgroup_id, node_id)
    output.ok(f"Added node {node_id} to subgroup {subgroup_id}")


@subgroup.command('remove-node')
@click.argument('group_id')
@click.argument('subgroup_id')
@click.argument('node_id')
@pass_user
def subgroup_remove_node(user, group_id, subgroup_id, node_id):
    """Remove NODE_ID from SUBGROUP_ID."""
    Group(user).remove_node_from_subgroup(group_id, subgroup_id, node_id)
    output.ok(f"Removed node {node_id} from subgroup {subgroup_id}")


@subgroup.command('share')
@click.argument('group_id')
@click.argument('subgroup_id')
@click.option('--username', help='Invitee. Omit for a QR-code share.')
@pass_user
def subgroup_share(user, group_id, subgroup_id, username):
    """Share SUBGROUP_ID.

    A subgroup share carries no access type of its own: it is always scoped to that one subgroup.
    """
    api = Group(user)
    try:
        if username:
            request_id = api.share_subgroup(group_id, subgroup_id, username)
            output.ok(f"Shared subgroup {subgroup_id} with {username}")
        else:
            request_id = api.share_subgroup_by_qr_code(group_id, subgroup_id)
            output.ok(f"Created a QR-code sharing request for subgroup {subgroup_id}")
    except AssertionError as e:
        output.fail(f"Failed to share subgroup {subgroup_id}: {e}")
    output.emit_kv(None, {'request_id': request_id})
