# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""The one place morpheus writes to a terminal.

Two contracts hold everything else together. Human mode puts prose and payloads on stdout. `--json`
puts the payload on stdout and every other word on stderr, so a shell script can pipe morpheus into
jq without filtering out progress lines. Both modes report failure through the exit code.
"""

import json as _json
import sys
import threading

import click
from rich.console import Console, Group
from rich.syntax import Syntax
from rich.table import Table
from rich.text import Text

EXIT_OK = 0
EXIT_FAILURE = 1
EXIT_USAGE = 2
EXIT_AUTH = 3


class CommandError(click.ClickException):
    """A command that ran and failed. Exits 1."""

    exit_code = EXIT_FAILURE

    def show(self, file=None):
        err(self.format_message())


class AuthError(CommandError):
    """Authentication or authorisation failed. Exits 3, so a script can retry credentials."""

    exit_code = EXIT_AUTH


class _State:
    json_mode = False
    raw = False
    verbose = 0
    # A subscription delivers on the MQTT thread while a command prints from the main one.
    # Anything that writes more than once holds this, so one block cannot land inside another.
    lock = threading.RLock()
    # Where the payload goes. Under --json this is the real stdout, kept aside while sys.stdout
    # itself is pointed at stderr.
    payload_stream = None
    out = Console(soft_wrap=True)
    msg = Console(soft_wrap=True)


_state = _State()


def configure(json_mode=False, raw=False, verbose=0):
    _state.json_mode = json_mode
    _state.raw = raw
    _state.verbose = verbose

    # The SDK's request trace carries bearer tokens, so it is opt-in behind -v.
    from ..sdk import user as _user_sdk
    _user_sdk.request_logging = bool(verbose)

    # The device trace is progress detail: route it to -v. A protocol event is a result and
    # stays visible.
    from ..sdk import device as _device_sdk
    _device_sdk.set_log_sink(trace)
    _device_sdk.set_event_sink(event)

    if _state.payload_stream is not None:
        sys.stdout = _state.payload_stream
        _state.payload_stream = None

    if json_mode:
        # Under --json, stdout carries the payload and nothing else. The SDK prints progress and
        # protocol errors on stdout of its own accord, so redirect the stream rather than chase
        # every call site: anything that is not emit_* lands on stderr with the rest of the noise.
        _state.payload_stream = sys.stdout
        sys.stdout = sys.stderr

    _state.out = _payload_console(_state.payload_stream if json_mode else None)
    _state.msg = Console(stderr=json_mode, soft_wrap=True)


def _payload_console(stream):
    """The console the payload prints on, coloured only for a real terminal.

    @note Colour is decided here rather than by rich, whose FORCE_COLOR support would write escape
    codes into a piped payload that something is about to parse.
    """
    try:
        is_tty = (stream or sys.stdout).isatty()
    except (AttributeError, ValueError):
        is_tty = False
    return Console(file=stream, soft_wrap=True, force_terminal=is_tty)


def json_mode():
    """Whether stdout is reserved for the payload. Governs stream discipline, not layout."""
    return _state.json_mode


def structured():
    """Whether a payload prints as JSON rather than a curated layout.

    `--json` implies it, because a machine reads that. `--raw` asks for it on a terminal, to see
    what the API actually returned rather than the fields a command chose to show.
    """
    return _state.json_mode or _state.raw


# --- messages: never stdout under --json ------------------------------------

def ok(message):
    _state.msg.print(f"[green]✓[/green] {message}")


def err(message):
    _state.msg.print(f"[red]✗[/red] {message}")


def info(message):
    _state.msg.print(message)


def warn(message):
    _state.msg.print(f"[yellow]![/yellow] {message}")


def debug(message):
    if _state.verbose:
        _state.msg.print(f"[dim]{message}[/dim]")


def trace(message):
    """A line of the SDK's own progress detail, shown under -v.

    @note The text is printed literally: it carries `[reported]` and other words rich would read
    as markup.
    """
    if _state.verbose:
        _state.msg.print(message, markup=False, highlight=False, style='dim')


def plain(message=''):
    """Literal text, unstyled. For the guide output, which is a formatted document already."""
    _state.msg.print(message, markup=False, highlight=False)


def event(message):
    """A protocol event the node reports as text, such as a shadow update it was told about.

    @note Not gated on -v: an event is something that happened, not a note about progress.
    """
    _state.msg.print(message, markup=False, highlight=False)


def block():
    """Hold the terminal for a group of writes that must stay together.

    @note Two threads write here: a command on the main one, a subscription on the MQTT one.
    Whoever writes more than once takes this, so the two orders interleave but never split.
    """
    return _state.lock


def inbound(topic, message):
    """A message that arrived on a subscription, printed where it arrived.

    @note One write, on the message channel. One write because the prompt is redrawn after each
    of them; the message channel because it is no command's result, so a script reading stdout
    must not find it among the payloads.
    """
    with block():
        _state.msg.print(Group(
            Text(f"<-- {topic}", style='dim'),
            Syntax(_json.dumps(message, indent=2, default=str), 'json',
                   theme='ansi_dark', background_color='default'),
        ))


# --- payloads: always stdout ------------------------------------------------

def emit_json(payload):
    """The machine-readable result of a command."""
    if _state.json_mode:
        click.echo(_json.dumps(payload, indent=None, default=str), file=_state.payload_stream)
    else:
        _state.out.print(Syntax(_json.dumps(payload, indent=2, default=str), 'json',
                                theme='ansi_dark', background_color='default'))


def emit_kv(title, mapping, min_width=0):
    """A flat block of fields — a Matter fabric, a claim result, a platform record.

    `min_width` pins the key column, so successive blocks describing the same kind of record line
    up with each other instead of each sizing itself to its own longest key.
    """
    if structured():
        emit_json(mapping)
        return
    width = max((len(str(k)) for k in mapping), default=0)
    width = max(width, min_width)
    with block():
        _emit_fields(title, mapping, width)


def _emit_fields(title, mapping, width):
    if title:
        _state.out.print(f"[bold]{title}[/bold]")
    for key, value in mapping.items():
        if isinstance(value, (list, tuple)):
            # A list of ids reads as one per line. These stay in the value column: they are fields,
            # and each is short enough to select on its own.
            first, *rest = [str(item) for item in value] or ['']
            _state.out.print(f"  {str(key).ljust(width)}  {first}", markup=False, highlight=False)
            for item in rest:
                _state.out.print(f"  {' ' * width}  {item}", markup=False, highlight=False)
            continue

        text = str(value)
        if '\n' in text:
            # A PEM certificate or a private key is a block, not a field. Label it, then print it
            # at column zero: indenting it would line up with the other values but put leading
            # spaces on every line of anything selected out of the terminal.
            _state.out.print(f"  {key}:", markup=False, highlight=False)
            _state.out.print(text, markup=False, highlight=False)
        else:
            _state.out.print(f"  {str(key).ljust(width)}  {text}", markup=False, highlight=False)


def emit_table(columns, rows, title=None):
    if structured():
        emit_json([dict(zip(columns, row)) for row in rows])
        return
    # No rules or borders: a table sits beside the key/value blocks, and box drawing next to them
    # reads as a different tool's output. Two spaces of padding matches their key column.
    table = Table(title=title, box=None, pad_edge=False, padding=(0, 2), header_style='bold')
    for column in columns:
        table.add_column(column, overflow='fold')
    for row in rows:
        table.add_row(*[str(cell) for cell in row])
    _state.out.print(table)


def emit_response(response, success_message=None, ok_statuses=(200, 201, 202, 204),
                  show_body=True):
    """Report an HTTP response, then fail the command when the status says it failed.

    Replaces the `print(f"Response: {code}")` + `print(response.text)` pair, which reported a 403
    exactly as loudly as a 200 and always exited 0.
    """
    body = _decode(response)
    if response.status_code not in ok_statuses:
        if body is not None:
            emit_json(body)
        raise _error_for(response, body)

    if body is None:
        ok(success_message or f"{response.status_code} {response.reason or ''}".strip())
        return None

    with block():
        if success_message:
            ok(success_message)
        if show_body:
            emit_json(body)
    return body


def _decode(response):
    text = (response.text or '').strip()
    if not text:
        return None
    try:
        return response.json()
    except ValueError:
        return {'raw': text}


def _error_for(response, body):
    detail = ''
    if isinstance(body, dict):
        detail = body.get('description') or body.get('message') or body.get('raw') or ''
    message = f"HTTP {response.status_code}{f': {detail}' if detail else ''}"
    if response.status_code in (401, 403):
        return AuthError(message)
    return CommandError(message)


def fail(message):
    """Abort the current command with exit code 1."""
    raise CommandError(message)


def missing_credentials(settings=None):
    """The message for a command that turned out to need AWS credentials and found none.

    Most of morpheus needs none, so this names the command's own need rather than implying the
    tool requires credentials to run at all.
    """
    where = ''
    if settings is not None:
        where = f" for account {settings.account_id} in {settings.region}"
    return CommandError(
        f"This command reaches AWS directly and found no credentials{where}. Configure them "
        "(AWS_PROFILE, aws sso login, ~/.aws/credentials, or environment variables). Commands "
        "that only use the deployment's API, such as `user` and `device`, need none.")


def read_json_file(path, what='file'):
    """Load a JSON file, reporting a missing or malformed one as a command failure."""
    try:
        with open(path, 'r') as handle:
            return _json.load(handle)
    except FileNotFoundError:
        fail(f"{what.capitalize()} not found: {path}")
    except _json.JSONDecodeError as e:
        fail(f"Invalid JSON in {path}: {e}")
    except OSError as e:
        fail(f"Could not read {path}: {e}")


def parse_json_arg(text, path=None):
    """A JSON payload given inline or with --file."""
    if path:
        return read_json_file(path, 'payload file')
    try:
        return _json.loads(text)
    except _json.JSONDecodeError as e:
        fail(f"Invalid JSON: {e}")


def is_tty():
    return sys.stdout.isatty()
