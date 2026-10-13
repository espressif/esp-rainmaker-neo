# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""click trees over the two simulators, so they share the `esp-morpheus` shell."""

import functools

import click

from . import output, shell


def needs_group(f):
    """Refuse a command that acts on the selected group when nothing is selected."""
    @functools.wraps(f)
    def wrapper(*args, **kwargs):
        simulator = click.get_current_context().obj
        if not simulator.selected_group:
            output.fail("No group selected. Run `select` first.")
        return f(*args, **kwargs)
    return wrapper


def _sim(ctx=None):
    return (ctx or click.get_current_context()).obj


def _json_arg(data, file_path=None):
    return output.parse_json_arg(' '.join(data), file_path)


# --- app simulator ----------------------------------------------------------

@click.group('app-sim')
def app_shell():
    """Drive the deployment the way the phone app does."""


@app_shell.command('list')
def app_list():
    """List the groups this user can see."""
    _sim().list_groups()


@app_shell.command('select')
@click.argument('group_id', required=False)
def app_select(group_id):
    """Select GROUP_ID, or the first group when it is omitted."""
    _sim().select_home(group_id)


@app_shell.command('stats')
def app_stats():
    """Show the running API, MQTT and shadow operation counts."""
    _sim()._print_stats()


@app_shell.command('update')
@click.argument('device')
@click.argument('payload', nargs=-1, required=True)
@needs_group
def app_update(device, payload):
    """Publish PAYLOAD to DEVICE on the selected group's params topic."""
    _sim().handle_device_command('update', [device, *payload])


@app_shell.command('update-group')
@click.argument('payload', nargs=-1, required=True)
@needs_group
def app_update_group(payload):
    """Publish PAYLOAD to the selected group's control topic."""
    _sim().handle_update_group_command(list(payload))


@app_shell.command('update-subgroup')
@click.argument('subgroup_id')
@click.argument('payload', nargs=-1, required=True)
@needs_group
def app_update_subgroup(subgroup_id, payload):
    """Publish PAYLOAD to SUBGROUP_ID's control topic."""
    _sim().handle_update_subgroup_command([subgroup_id, *payload])


@app_shell.group('schedule')
def app_schedule():
    """Read and write a node's schedules."""


@app_schedule.command('set')
@click.argument('device')
@click.argument('payload', nargs=-1, required=True)
@needs_group
def schedule_set(device, payload):
    """Set DEVICE's schedules from PAYLOAD, a JSON list of schedule objects."""
    _sim().handle_schedule_command(['set', device, *payload])


@app_schedule.command('get')
@click.argument('device')
@needs_group
def schedule_get(device):
    """Show DEVICE's schedules."""
    _sim().handle_schedule_command(['get', device])


@app_schedule.command('delete')
@click.argument('device')
@needs_group
def schedule_delete(device):
    """Delete DEVICE's schedules."""
    _sim().handle_schedule_command(['delete', device])


@app_shell.group('automation')
def app_automation():
    """Build and manage automations on the selected group."""


@app_automation.command('create')
@click.argument('name', nargs=-1, required=True)
@needs_group
def automation_create(name):
    """Create an empty automation called NAME."""
    _sim().handle_automation_command(['create', *name])


@app_automation.command('add-trigger')
@click.argument('automation_id')
@click.argument('node_id')
@click.argument('device')
@click.argument('param')
@click.argument('operator')
@click.argument('value')
@needs_group
def automation_add_trigger(automation_id, node_id, device, param, operator, value):
    """Add a trigger to AUTOMATION_ID."""
    _sim().handle_automation_command(
        ['add-trigger', automation_id, node_id, device, param, operator, value])


@app_automation.command('add-action')
@click.argument('automation_id')
@click.argument('node_id')
@click.argument('path')
@click.argument('value')
@needs_group
def automation_add_action(automation_id, node_id, path, value):
    """Add an action to AUTOMATION_ID."""
    _sim().handle_automation_command(['add-action', automation_id, node_id, path, value])


@app_automation.command('complete')
@click.argument('automation_id')
@needs_group
def automation_complete(automation_id):
    """Finalise AUTOMATION_ID."""
    _sim().handle_automation_command(['complete', automation_id])


@app_automation.command('list')
@needs_group
def automation_list():
    """List the automations on the selected group."""
    _sim().handle_automation_command(['list'])


@app_automation.command('get')
@click.argument('automation_id')
@needs_group
def automation_get(automation_id):
    """Show AUTOMATION_ID."""
    _sim().handle_automation_command(['get', automation_id])


@app_automation.command('delete')
@click.argument('automation_id')
@needs_group
def automation_delete(automation_id):
    """Delete AUTOMATION_ID."""
    _sim().handle_automation_command(['delete', automation_id])


@app_shell.command('prov')
@click.argument('name_prefix', required=False)
@click.option('--group', 'group_id', help='Select this group first.')
@click.option('--device', help='Exact BLE name of the device. Skips the scan and the picker.')
@click.option('--pop', envvar='RMNG_PROV_POP', help='Proof of possession. Also read from RMNG_PROV_POP.')
@click.option('--ssid', help='Wi-Fi SSID.')
@click.option('--passphrase', envvar='RMNG_WIFI_PASSPHRASE', help='Wi-Fi passphrase. Also read from RMNG_WIFI_PASSPHRASE.')
@click.pass_context
def app_prov(ctx, name_prefix, group_id, device, pop, ssid, passphrase):
    """Provision a BLE device into the selected group. Any value not given as an option is prompted for.

    Vendors esp_prov on first use. IDF_PATH, when set, supplies the protocomm protobuf files.
    """
    simulator = _sim()
    if group_id and not simulator.select_home(group_id):
        output.fail(f"Could not select group {group_id}")
    if not simulator.selected_group:
        output.fail("No group selected. Run `select` first, or pass --group.")
    if not simulator.handle_prov_command([name_prefix] if name_prefix else [], device=device, pop=pop,
                                         ssid=ssid, passphrase=passphrase):
        ctx.exit(1)


# --- device simulator -------------------------------------------------------

@click.group('device-sim')
def device_shell():
    """Drive the deployment the way a node does."""


@device_shell.command('update-params')
@click.argument('payload', nargs=-1, required=True)
@click.option('--file', 'file_path', type=click.Path(exists=True, dir_okay=False),
              help='Read the JSON payload from a file.')
def device_update_params(payload, file_path):
    """Update the node's shadows from PAYLOAD, keyed by device name."""
    data = _json_arg(payload, file_path)
    if not isinstance(data, dict):
        output.fail(f"Expected a JSON object keyed by device name, got {type(data).__name__}")
    if not _sim().update_shadows(data):
        output.fail('Failed to update the shadows')
    output.ok('Updated the shadows')


@device_shell.command('update-tags')
@click.argument('payload', nargs=-1, required=True)
@click.option('--file', 'file_path', type=click.Path(exists=True, dir_okay=False),
              help='Read the JSON payload from a file.')
def device_update_tags(payload, file_path):
    """Update the node's tags from PAYLOAD."""
    simulator = _sim()
    data = _json_arg(payload, file_path)
    if not simulator.device.update_named_shadow(simulator.ishadow_name,
                                                simulator._tags_to_update_payload(data)):
        output.fail('Failed to update the tags')
    output.ok('Updated the tags')


def run_once(simulator, group, args):
    """Start a simulator, run one command line against `group`, and stop it; errors keep their exit code."""
    if not simulator.start():
        output.fail('Failed to start the simulator')
    try:
        with click.Context(group, obj=simulator, info_name=group.name) as ctx:
            name, command, rest = group.resolve_command(ctx, list(args))
            with command.make_context(name, rest, parent=ctx) as sub:
                command.invoke(sub)
    finally:
        simulator.stop()


def run(simulator, group, label, history):
    """Start a simulator, then hand its command tree to the shell."""
    if not simulator.start():
        output.fail('Failed to start the simulator')
    try:
        with click.Context(group, obj=simulator, info_name=group.name) as ctx:
            shell.run(group, ctx, f"{label} > ", history)
    finally:
        simulator.stop()
