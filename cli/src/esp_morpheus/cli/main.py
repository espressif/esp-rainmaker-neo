# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""The `morpheus` command.

`user`, `device` and `admin` take an identity and then either run one subcommand and exit, or —
given no subcommand — open a REPL bound to that identity. The same click tree serves both, so a
command reachable from the shell is reachable from a script, and vice versa.

`admin` is the whole privileged surface: anything that needs AWS credentials or the admin API sits
under it, so what the rest of the tree can reach is what an ordinary account can reach.
"""

import traceback

import click
from botocore.exceptions import NoCredentialsError

from .. import __version__, paths
from ..sdk.errors import NotReadyError
from . import device as device_commands
from . import groups, matter, nodes, output, sharing, sims
from . import user as user_commands
from .admin import admin
from .context import Session
from .shell import ContextGroup

CONTEXT_SETTINGS = {'help_option_names': ['-h', '--help'], 'max_content_width': 100}


@click.group(context_settings=CONTEXT_SETTINGS)
@click.option('--client-outputs', envvar='MORPHEUS_OUTPUTS', metavar='SOURCE',
              help='Deployment outputs: a file path or an http(s) URL, such as a published '
                   'rmng-client-outputs.json. Default: rmng-outputs.json.')
@click.option('--json', 'json_mode', is_flag=True, envvar='MORPHEUS_JSON',
              help='Put the payload on stdout and everything else on stderr.')
@click.option('--raw', is_flag=True,
              help="Print each payload as JSON rather than a curated layout, to see what the API "
                   "returned. --json implies this.")
@click.option('-v', '--verbose', count=True,
              help='Show the API request trace, progress detail and tracebacks.')
@click.version_option(__version__, '-V', '--version')
@click.pass_context
def cli(ctx, client_outputs, json_mode, raw, verbose):
    """Console client for an ESP RainMaker Neo deployment."""
    output.configure(json_mode=json_mode, raw=raw, verbose=verbose)
    ctx.obj = Session(outputs_source=client_outputs, json_mode=json_mode, verbose=verbose)


@cli.group(cls=ContextGroup, invoke_without_command=True,
           repl_prompt='{label} > ', history='user')
@click.argument('identity', required=False, metavar='IDENTITY')
@click.option('--password', help='Password for an identity that test_config.json does not carry. '
                                 'Also read from RMNG_PASSWORD, else prompted for. Prefer either '
                                 'over argv, which other processes and shell history can see.')
@click.pass_context
def user(ctx, identity, password):
    """Act as IDENTITY, an end user: an email address, or an index or name in test_config.json.

    This tree is the end-user API and nothing else. The privileged surface is `morpheus admin`.
    With no subcommand, this opens an interactive prompt bound to that identity.
    """
    if identity is None:
        raise click.MissingParameter(ctx=ctx, param_hint='IDENTITY', param_type='argument')
    session = ctx.find_object(Session)
    session.password = password
    session.select_user(identity)
    if ctx.invoked_subcommand is None:
        # Warn rather than abort: an account that does not exist yet cannot sign in, and `auth`
        # from this prompt is how it gets created.
        user = session.authenticated_user(on_failure='warn')
        output.info(f"User context: [bold]{user.username}[/bold]")
        ctx.command.open_shell(ctx, user.username)


@cli.group(cls=ContextGroup, invoke_without_command=True,
           repl_prompt='{label} > ', history='device')
@click.argument('node', required=False, metavar='NODE')
@click.pass_context
def device(ctx, node):
    """Act as NODE: an index or thing name in test_config.json.

    With no subcommand, this opens an interactive prompt bound to that node.
    """
    if node is None:
        raise click.MissingParameter(ctx=ctx, param_hint='NODE', param_type='argument')
    session = ctx.find_object(Session)
    session.select_device(node)
    if ctx.invoked_subcommand is None:
        output.info(f"Device context: [bold]{session.device.node_thing_name}[/bold]")
        ctx.command.open_shell(ctx, session.device.node_thing_name)


# --- simulators -------------------------------------------------------------

@cli.command('app-sim')
@click.argument('identity')
@click.option('--password', help='Password for an identity that test_config.json does not carry. '
                                 'Also read from RMNG_PASSWORD, else prompted for.')
@click.pass_context
def app_sim(ctx, identity, password):
    """Run the sequence of user operations a real phone app performs, as IDENTITY.

    IDENTITY is an end user: an email address, or an index or name in test_config.json.
    """
    from ..sims.app import AppSim

    session = ctx.find_object(Session)
    session.password = password
    session.select_user(identity)
    # The identity resolves the same way `morpheus user` resolves it — prompting for a password and
    # checking it — rather than through the simulator's own copy of the lookup.
    sims.run(AppSim(identity, user=session.authenticated_user(),
                    rmng_outputs_path=session.settings.source),
             sims.app_shell, f"app:{identity}", 'app_sim')


@cli.command('device-sim')
@click.argument('node')
@click.pass_context
def device_sim(ctx, node):
    """Run the sequence of device operations a real node performs, as NODE."""
    from ..sims.device import DeviceSim

    session = ctx.find_object(Session)
    sims.run(DeviceSim(node, config_path=str(paths.test_config_path()),
                       rmng_outputs_path=session.settings.source),
             sims.device_shell, f"node:{node}", 'device_sim')


# --- tree assembly ----------------------------------------------------------

for command in user_commands.COMMANDS:
    user.add_command(command)
user.add_command(groups.group_cmd)
user.add_command(nodes.node)
user.add_command(matter.matter)
user.add_command(sharing.sharing)
user.add_command(sharing.push)

for command in device_commands.COMMANDS:
    device.add_command(command)

cli.add_command(admin)


def main():
    """Entry point that keeps a traceback off the terminal unless -v was given."""
    try:
        cli(standalone_mode=True)
    except NoCredentialsError:
        error = output.missing_credentials()
        error.show()
        raise SystemExit(error.exit_code)
    except NotReadyError as e:
        output.err(f"Failed: {e}")
        raise SystemExit(output.EXIT_FAILURE)
    except Exception as e:  # noqa: BLE001
        output.err(f"{type(e).__name__}: {e}")
        output.debug(traceback.format_exc())
        raise SystemExit(output.EXIT_FAILURE)


if __name__ == '__main__':
    main()
