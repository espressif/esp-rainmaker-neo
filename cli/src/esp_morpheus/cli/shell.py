# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""An interactive prompt over a click group.

The completer, the help text and the dispatcher all read the same click command tree, so a command
cannot exist without appearing in completion and help.

Dispatch reuses the group's already-built context, so `morpheus user alice` resolves the identity
once and every line typed afterwards runs against it.
"""

import shlex
import traceback

import click
from botocore.exceptions import NoCredentialsError
from prompt_toolkit import PromptSession
from prompt_toolkit.auto_suggest import AutoSuggestFromHistory
from prompt_toolkit.completion import NestedCompleter
from prompt_toolkit.history import FileHistory
from prompt_toolkit.patch_stdout import patch_stdout

from .. import paths
from ..sdk.errors import NotReadyError
from . import output

EXIT_WORDS = ('q', 'quit', 'exit')


def _visible(group, ctx):
    """Subcommands of `group` a user may run here, in the order click lists them."""
    for name in group.list_commands(ctx):
        command = group.get_command(ctx, name)
        if command is not None and not command.hidden:
            yield name, command


def _subcommand_tree(group, ctx, depth=3):
    """A nested dict of command names, for NestedCompleter."""
    tree = {}
    for name, command in _visible(group, ctx):
        tree[name] = (_subcommand_tree(command, ctx, depth - 1)
                      if isinstance(command, click.Group) and depth > 1 else None)
    return tree


def _completion_tree(group, ctx):
    """The command tree plus the words only the shell itself understands."""
    tree = _subcommand_tree(group, ctx)
    tree['help'] = {name: None for name, _ in _visible(group, ctx)}
    for word in EXIT_WORDS:
        tree[word] = None
    return tree


def _print_help(group, ctx, args):
    """`help` lists the tree; `help <command>` prints click's own help for it."""
    if args:
        try:
            name, command, rest = group.resolve_command(ctx, list(args))
        except click.UsageError as e:
            output.err(e.format_message())
            return
        # A group named alone shows its own help; a subcommand path recurses into it.
        while rest and isinstance(command, click.Group):
            try:
                name, command, rest = command.resolve_command(ctx, rest)
            except click.UsageError as e:
                output.err(e.format_message())
                return
        with click.Context(command, info_name=name, parent=ctx) as sub:
            output.plain(command.get_help(sub))
        return

    output.plain(group.get_short_help_str(limit=100) or '')
    output.plain('Commands:')
    names = list(_visible(group, ctx))
    width = max((len(n) for n, _ in names), default=0)
    for name, command in names:
        output.plain(f"  {name.ljust(width)}  {command.get_short_help_str(limit=90)}")
    output.plain('')
    output.plain(f"  {'help [COMMAND]'.ljust(width)}  Show this list, or one command's full help")
    output.plain(f"  {'q | quit'.ljust(width)}  Leave this context")


def split_line(line):
    """Split a command line like a shell, but keep a bare JSON payload verbatim.

    shlex strips the double quotes out of an unquoted `{"a":1}`, so everything from the token that
    opens one is taken as a single argument.
    """
    start = _payload_start(line)
    if start is None:
        return shlex.split(line)
    return shlex.split(line[:start]) + [line[start:].strip()]


def _payload_start(line):
    """Where a bare JSON object or array starts a token in `line`, or None."""
    quote = None
    fresh = True
    for index, char in enumerate(line):
        if quote:
            quote = None if char == quote else quote
        elif char in '"\'':
            quote, fresh = char, False
        elif char.isspace():
            fresh = True
        elif fresh:
            if char in '{[':
                return index
            fresh = False
    return None


def run(group, ctx, prompt, history_name):
    """Read lines and dispatch them into `group` until the user quits.

    One command may not end the session: a usage error, a raised exception and Ctrl-C all
    re-prompt. Only EOF (Ctrl-D) and an explicit quit leave.
    """
    session = PromptSession(
        history=FileHistory(str(paths.history_path(history_name))),
        auto_suggest=AutoSuggestFromHistory(),
        completer=NestedCompleter.from_nested_dict(_completion_tree(group, ctx)),
        complete_while_typing=True,
    )

    output.info("Type [bold]help[/bold] for commands, [bold]q[/bold] to leave.")
    while True:
        try:
            # A subscription delivers on the MQTT thread: print above the prompt, not over it.
            # raw, because the default proxy rewrites every escape it is given to `?`.
            with patch_stdout(raw=True):
                line = session.prompt(prompt)
        except KeyboardInterrupt:
            continue
        except EOFError:
            return

        try:
            args = split_line(line)
        except ValueError as e:
            output.err(f"Could not parse the line: {e}")
            continue

        if not args:
            continue
        if args[0].lower() in EXIT_WORDS:
            return
        if args[0].lower() == 'help':
            # A listing is many writes, and a subscription may deliver in the middle of it.
            with output.block():
                _print_help(group, ctx, args[1:])
            continue

        dispatch(group, ctx, args)


def dispatch(group, ctx, args):
    """Run one already-split command line against `group`, containing every failure.

    click raises rather than exits because the command is resolved and invoked directly instead of
    through `main()`, which would call sys.exit on the first bad flag.
    """
    try:
        name, command, rest = group.resolve_command(ctx, args)
        # A group named on its own is a request to see what is under it, not a mistake.
        if isinstance(command, click.Group) and not rest:
            _print_help(group, ctx, args)
            return
        with command.make_context(name, rest, parent=ctx) as sub:
            command.invoke(sub)
    except click.exceptions.Exit:
        pass
    except click.UsageError as e:
        message = e.format_message()
        output.err(message)
        # click renders a group's "missing subcommand" as its whole help, usage line included.
        if e.ctx is not None and not message.lstrip().startswith('Usage:'):
            output.plain(e.ctx.get_usage())
    except click.ClickException as e:
        e.show()
    except NoCredentialsError:
        output.missing_credentials().show()
    except NotReadyError as e:
        # Only outside `pass_device`, which words a remedy for the commands it wraps.
        output.err(f"Failed: {e}")
    except SystemExit as e:
        # A stray process exit from a library: one command may not end the session.
        output.err(e.code if isinstance(e.code, str) else f"Command exited ({e.code}).")
    except click.Abort:
        output.plain()
    except KeyboardInterrupt:
        output.plain()
    except Exception as e:  # noqa: BLE001 - a bad command must not end the session
        output.err(f"{type(e).__name__}: {e}")
        output.debug(traceback.format_exc())
