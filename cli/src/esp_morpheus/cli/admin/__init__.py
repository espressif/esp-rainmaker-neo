# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus admin ...` — every operation that needs AWS credentials or the admin API.

One tree holds the whole privileged surface, so what an ordinary caller can reach is exactly what
sits outside it: `user` and `device` go through the deployment's own API and broker and need no AWS
account, while everything here either signs in against the admin pool or calls AWS directly.

The identity is optional, because half of these commands need no identity at all — `ses`, `sns`,
`bot-user` and `test-data` reach AWS, not the admin API.
"""

import os

import boto3
import click
from botocore.exceptions import BotoCoreError, ClientError

from .. import output
from ..context import Session
from ..guide import guide
from ..shell import ContextGroup
from . import (botuser, gendevice, integrations, messaging, nodes, platforms, runtime,
               testdata)

IDENTITY_HINT = ('name one with `morpheus admin <identity>`, or seed one with '
                 '`morpheus admin test-data setup`.')

# Where the group leaves the identity it took off the command line, for its own callback to read.
IDENTITY_META = 'morpheus.admin.identity'


def _configured_admin(session):
    """The admin test_config.json flags, so the seeded deployment needs no identity."""
    return next((u.get('name') for u in session.config.get('users', []) if u.get('admin')), None)


def _deployment_row(session, settings):
    """Which deployment every command here acts on."""
    if settings is None:
        return '[red]unresolved[/red]', session.settings_error
    return f"{settings.account_id} / {settings.region}", _source_label(settings.source)


def _source_label(source):
    """The outputs file, written relative to the working directory when it sits below it."""
    if source.startswith(('http://', 'https://')):
        return source
    relative = os.path.relpath(source)
    return source if relative.startswith(os.pardir) else relative


def _admin_api_row(session, identity, settings):
    """The admin account as (value, note, username), the last being what the prompt is labelled with.

    Sign-in is reported, not enforced: the AWS half of this tree runs without an admin account.
    """
    if not identity:
        return '[yellow]none[/yellow]', IDENTITY_HINT, None
    if settings is None:
        return identity, 'not signed in: the deployment is unresolved', None
    # Warn rather than abort: an admin that does not exist yet is created by `test-data setup`.
    user = session.authenticated_user(on_failure='warn')
    note = 'signed in' if getattr(user, 'token', None) else '[red]sign-in failed[/red]'
    return f"[bold]{user.username}[/bold]", note, user.username


def _aws_row():
    """Where the ambient credentials come from, without spending an STS call to find out.

    Resolving them for real would force credentials on a prompt whose admin-API half needs none.
    """
    try:
        credentials = boto3.session.Session().get_credentials()
    except (BotoCoreError, ClientError) as e:
        return '[red]unusable[/red]', str(e)
    if credentials is None:
        return ('[yellow]none found[/yellow]',
                'configure AWS_PROFILE, `aws sso login`, ~/.aws/credentials or the environment')
    profile = os.environ.get('AWS_PROFILE')
    source = getattr(credentials, 'method', None) or 'unknown source'
    where = f"{source}, AWS_PROFILE={profile}" if profile else source
    return where, 'account checked on the first AWS command'


def _print_context(session, identity):
    """Name both identities this context holds, and return the one the prompt is labelled with.

    Every row is built before anything prints, so a password prompt cannot land inside the banner.
    """
    settings = session.try_settings()
    api_value, api_note, username = _admin_api_row(session, identity, settings)
    output.context_rows('Admin context', [
        ('deployment', *_deployment_row(session, settings)),
        ('admin API', api_value, api_note),
        ('AWS creds', *_aws_row()),
    ])
    return username


class AdminGroup(ContextGroup):
    """`admin [IDENTITY] <command>`, where the identity may be left out.

    click would read the first word as the identity and then find no command, so a first word that
    names one of these commands is that command.
    """

    def parse_args(self, ctx, args):
        if args and not args[0].startswith('-') and args[0] not in self.commands:
            ctx.meta[IDENTITY_META] = args[0]
            args = args[1:]
        return super().parse_args(ctx, args)

    def resolve_command(self, ctx, args):
        """Report a word taken as the identity, which is what a mistyped command becomes here."""
        try:
            return super().resolve_command(ctx, args)
        except click.UsageError:
            identity = ctx.meta.get(IDENTITY_META)
            if not identity:
                raise
            raise click.UsageError(
                f"No such command {args[0]!r}. The first word is read as the identity "
                f"({identity!r}), so the command follows it.", ctx=ctx) from None


@click.group('admin', cls=AdminGroup, invoke_without_command=True,
             options_metavar='', subcommand_metavar='[IDENTITY] [OPTIONS] COMMAND [ARGS]...',
             repl_prompt='{label} > ', history='admin')
@click.option('--password', help='Password for an identity that test_config.json does not carry. '
                                 'Also read from RMNG_PASSWORD, else prompted for. Prefer either '
                                 'over argv, which other processes and shell history can see.')
@click.pass_context
def admin(ctx, password):
    """Administer the deployment, as IDENTITY where a command needs one.

    IDENTITY is an admin: an email address, or an index or name in test_config.json. It defaults to
    the admin test_config.json flags, and only the commands that call the admin API need one. With
    no subcommand, this opens an interactive prompt.
    """
    session = ctx.find_object(Session)
    session.is_admin = True
    session.password = password
    session.identity_hint = IDENTITY_HINT

    identity = ctx.meta.get(IDENTITY_META) or _configured_admin(session)
    if identity:
        session.select_user(identity)
    if ctx.invoked_subcommand is not None:
        return

    username = _print_context(session, identity)
    ctx.command.open_shell(ctx, f"admin:{username}" if username else 'admin')


# `guide` needs nothing itself: it prints the console half of a setup whose other half is here.
for command in (botuser.bot_user, guide, integrations.integrations, messaging.ses, messaging.sns,
                nodes.nodes, platforms.platforms, runtime.claiming, runtime.iot_event_mode,
                testdata.test_data):
    admin.add_command(command)

# Same reason, one level down: the node it writes is inert until `test-data setup` registers it.
testdata.test_data.add_command(gendevice.gen_device)
