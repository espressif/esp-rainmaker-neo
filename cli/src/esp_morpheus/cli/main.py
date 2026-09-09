# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""The `morpheus` command.

`user` and `device` take an identity and then either run one subcommand and exit, or — given no
subcommand — open a REPL bound to that identity. The same click tree serves both, so a command
reachable from the shell is reachable from a script, and vice versa.
"""

import traceback

import click
from botocore.exceptions import NoCredentialsError

from .. import __version__, paths
from . import device as device_commands
from . import groups, matter, nodes, output, sharing, shell, sims
from . import user as user_commands
from .admin import admin
from .context import Session
from .guide import guide
from .testdata import bot_user, gen_device, test_data

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


def _record_identity(ctx, param, value):
    """Bind the identity onto the session while parsing, not after.

    `--help` is eager, so it prints and exits before a group callback ever runs. The admin surface
    is hidden or shown by who is asking, and help is exactly when that must already be known — so
    these parameters are eager too, and record without resolving anything.
    """
    session = ctx.find_object(Session)
    if session is not None and value is not None:
        (session.select_device if param.name == 'node' else session.select_user)(value)
    return value


def _record_admin(ctx, param, value):
    session = ctx.find_object(Session)
    if session is not None:
        session.is_admin = value
    return value


class ContextGroup(click.Group):
    """A group that takes an identity, then runs a subcommand or opens a REPL over its own tree."""

    def __init__(self, *args, repl_prompt=None, history=None, **kwargs):
        super().__init__(*args, **kwargs)
        self.repl_prompt = repl_prompt
        self.history = history

    def open_shell(self, ctx, label):
        shell.run(self, ctx, self.repl_prompt.format(label=label), self.history)

    def resolve_command(self, ctx, args):
        """Say what to do when one of this group's own options follows the identity.

        click stops parsing a group's options at its first positional, so `user alice --admin auth`
        reaches here with `--admin` where a subcommand should be. Untreated that reads as an unknown
        command, which points at the wrong thing.
        """
        name = args[0] if args else ''
        if name.startswith('-'):
            for param in self.params:
                if name in getattr(param, 'opts', []) + getattr(param, 'secondary_opts', []):
                    metavar = self.params[0].make_metavar(ctx) if self.params else 'IDENTITY'
                    raise click.UsageError(
                        f"{name} is an option of `{ctx.info_name}`, so it goes before the "
                        f"identity: {ctx.command_path} {name} {metavar} ...", ctx=ctx)
        return super().resolve_command(ctx, args)


@cli.group(cls=ContextGroup, invoke_without_command=True,
           repl_prompt='{label} > ', history='user')
@click.argument('identity', required=False, metavar='IDENTITY', is_eager=True,
                callback=_record_identity)
@click.option('--password', help='Password for an identity that test_config.json does not carry. '
                                 'Also read from RMNG_PASSWORD, else prompted for. Prefer either '
                                 'over argv, which other processes and shell history can see.')
@click.option('--admin', 'is_admin', is_flag=True, is_eager=True, callback=_record_admin,
              help='Authenticate IDENTITY against the admin pool. Only needed for identities that '
                   'test_config.json does not already flag as admin.')
@click.pass_context
def user(ctx, identity, password, is_admin):
    """Act as IDENTITY: an email address, or an index or name in test_config.json.

    With no subcommand, this opens an interactive prompt bound to that identity.
    """
    if identity is None:
        raise click.MissingParameter(ctx=ctx, param_hint='IDENTITY', param_type='argument')
    session = ctx.find_object(Session)
    session.password = password
    if ctx.invoked_subcommand is None:
        # Warn rather than abort: an account that does not exist yet cannot sign in, and `auth`
        # from this prompt is how it gets created.
        user = session.authenticated_user(on_failure='warn')
        output.info(f"User context: [bold]{user.username}[/bold]")
        ctx.command.open_shell(ctx, user.username)


@cli.group(cls=ContextGroup, invoke_without_command=True,
           repl_prompt='{label} > ', history='device')
@click.argument('node', required=False, metavar='NODE', is_eager=True,
                callback=_record_identity)
@click.pass_context
def device(ctx, node):
    """Act as NODE: an index or thing name in test_config.json.

    With no subcommand, this opens an interactive prompt bound to that node.
    """
    if node is None:
        raise click.MissingParameter(ctx=ctx, param_hint='NODE', param_type='argument')
    session = ctx.find_object(Session)
    if ctx.invoked_subcommand is None:
        output.info(f"Device context: [bold]{session.device.node_thing_name}[/bold]")
        ctx.command.open_shell(ctx, session.device.node_thing_name)


# --- simulators -------------------------------------------------------------

@cli.command('app-sim')
@click.argument('identity')
@click.option('--password', help='Password for an identity that test_config.json does not carry. '
                                 'Also read from RMNG_PASSWORD, else prompted for.')
@click.option('--admin', 'is_admin', is_flag=True,
              help='Authenticate IDENTITY against the admin pool.')
@click.pass_context
def app_sim(ctx, identity, password, is_admin):
    """Run the sequence of user operations a real phone app performs, as IDENTITY.

    IDENTITY is an email address, or an index or name in test_config.json.
    """
    from ..sims.app import AppSim

    session = ctx.find_object(Session)
    session.password = password
    session.is_admin = is_admin
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
user.add_command(admin)
user.add_command(guide)

for command in device_commands.COMMANDS:
    device.add_command(command)

cli.add_command(test_data)
cli.add_command(bot_user)
cli.add_command(gen_device)
cli.add_command(guide)


def main():
    """Entry point that keeps a traceback off the terminal unless -v was given."""
    try:
        cli(standalone_mode=True)
    except NoCredentialsError:
        error = output.missing_credentials()
        error.show()
        raise SystemExit(error.exit_code)
    except Exception as e:  # noqa: BLE001
        output.err(f"{type(e).__name__}: {e}")
        output.debug(traceback.format_exc())
        raise SystemExit(output.EXIT_FAILURE)


if __name__ == '__main__':
    main()
