# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""The session one `morpheus` run drives: one deployment, one test_config.json, one identity.

Everything here was a module global in cli/morpheus.py, which forced the outputs file to be read and
the AWS identity to be checked before argparse had even chosen a command. The deployment now
resolves on first use, so `morpheus --help` and `morpheus admin guide alexa` need no credentials.
"""

import getpass
import json
import os
import secrets
import string

import click

from .. import paths
from ..outputs import OutputsError, RmngSettings, resolve_source, verify_aws_identity
from ..sdk.device import Device, generate_key_and_cert
from ..sdk.errors import NotReadyError
from ..sdk.user import User
from . import output

TEST_CONFIG_DEFAULTS = 'test_config.default.json'

# Tries at the prompt before giving up, counting the first. Only a typed password is re-asked for:
# one from --password or RMNG_PASSWORD came from a script, which cannot answer.
PASSWORD_ATTEMPTS = 3

# How a command that found no identity says where one comes from. `morpheus admin` replaces it,
# because its identity is an option and half its commands need none.
USER_IDENTITY_HINT = 'this command runs under `morpheus user <identity>`.'


def generate_password(length=16):
    """A random password satisfying Cognito complexity (upper, lower, digit, symbol)."""
    specials = '!@#$%^&*'
    alphabet = string.ascii_letters + string.digits + specials
    while True:
        password = ''.join(secrets.choice(alphabet) for _ in range(length))
        if (any(c.islower() for c in password) and any(c.isupper() for c in password)
                and any(c.isdigit() for c in password) and any(c in specials for c in password)):
            return password


class Session:
    """Deployment settings, test_config.json, and the identities resolved against them."""

    def __init__(self, outputs_source=None, json_mode=False, verbose=0):
        self.outputs_source = outputs_source
        self.json_mode = json_mode
        self.verbose = verbose
        self._settings = None
        self.settings_error = None
        self._aws_verified = False
        self._config = None
        # The `user` and `device` groups record which identity was asked for; the object is built
        # on first use, so `morpheus user 0 group --help` needs no credentials.
        self._user_identity = None
        self._device_identity = None
        self._user = None
        self._device = None
        self._password_prompted = False
        self.password = None
        # Only `morpheus admin` sets this: an identity given there that test_config.json does not
        # carry signs in against the admin pool. `user` and `app-sim` are the end-user API.
        self.is_admin = False
        self.identity_hint = USER_IDENTITY_HINT

    # --- the identity this invocation acts as -------------------------------

    def select_user(self, identity):
        self._user_identity = identity

    def select_device(self, identity):
        self._device_identity = identity

    @property
    def user(self):
        """The selected identity, built but not signed in.

        For `auth`, which provisions an account that may not exist yet: its first sign-in
        necessarily fails, and that failure is the path that creates the account.
        """
        if self._user is None:
            if self._user_identity is None:
                output.fail(f"No user selected; {self.identity_hint}")
            self._user = self.get_user(self._user_identity)
        return self._user

    def authenticated_user(self, on_failure='raise'):
        """The selected identity, signed in.

        Checking the password here rather than leaving it to whichever command runs first is what
        makes a typo recoverable: at the prompt it can be retyped, and in the shell the alternative
        is finding out after the session is already open, with no way to correct it.
        """
        user = self.user
        if getattr(user, 'token', None):
            return user

        for remaining in range(PASSWORD_ATTEMPTS - 1, -1, -1):
            if user.get_cognito_token():
                return user
            if not (remaining and self._password_prompted):
                break
            output.err(f"Authentication failed for {user.username}.")
            try:
                user.password = getpass.getpass(f"Password for {user.username}: ")
            except (EOFError, KeyboardInterrupt):
                output.plain()
                break

        message = (f"Authentication failed for {user.username}. Check the password, or run `auth` "
                   "if the account does not exist yet.")
        if on_failure == 'warn':
            output.warn(message)
            return user
        raise output.AuthError(message)

    @property
    def device(self):
        if self._device is None:
            if self._device_identity is None:
                output.fail('No device selected; this command runs under `morpheus device <node>`.')
            self._device = self.get_node(self._device_identity)
        return self._device

    # --- deployment ---------------------------------------------------------

    @property
    def settings(self) -> RmngSettings:
        """The deployment this run targets. Reading the outputs needs no AWS credentials."""
        if self._settings is None:
            # OutputsError exits the process, which in the REPL would end the session.
            try:
                self._settings = RmngSettings.from_source(self.outputs_source)
            except OutputsError as e:
                output.fail(e.message)
            except (OSError, ValueError) as e:
                # A missing or malformed file, or an unreachable URL: named, not raised raw.
                reason = getattr(e, 'strerror', None) or e
                output.fail(f"Could not read deployment outputs from "
                            f"{resolve_source(self.outputs_source)}: {reason}. Name another with "
                            "`--client-outputs <file|url>`.")
        return self._settings

    def try_settings(self):
        """The deployment, or None with the reason in `settings_error`.

        For the context banner, which reports an unresolved deployment rather than aborting on it.
        """
        try:
            return self.settings
        except output.CommandError as e:
            self.settings_error = e.format_message()
            return None

    def require_aws(self):
        """Check that the ambient AWS credentials describe this deployment.

        Only the commands that reach AWS directly call this. The user and device contexts are HTTP
        and mutual-TLS MQTT throughout — an end user authenticates against the deployment's own API
        and receives its credentials from it — so requiring credentials there locked out every
        caller who legitimately has none.
        """
        if not self._aws_verified:
            # A mismatch does not fail loudly on its own: it silently reads and mutates a different
            # deployment than the one named.
            try:
                verify_aws_identity(self.settings)
            except OutputsError as e:
                output.fail(e.message)
            self._aws_verified = True
        return self.settings

    @property
    def region(self):
        return self.settings.region

    # --- test_config.json ---------------------------------------------------

    @property
    def config(self):
        if self._config is None:
            self._config = self._read_config() or {}
        return self._config

    def _read_config(self):
        path = paths.test_config_path()
        try:
            with open(path, 'r') as handle:
                return json.load(handle)
        except FileNotFoundError:
            output.debug(f"No test_config.json at {path}; run `morpheus admin test-data setup` "
                         'to create it.')
            return None
        except json.JSONDecodeError as e:
            output.fail(f"Invalid JSON in {path}: {e}")

    def config_exists(self):
        return paths.test_config_path().exists()

    def write_config(self, config=None):
        path = paths.test_config_path()
        paths.ensure(path.parent)
        with open(path, 'w') as handle:
            json.dump(config if config is not None else self.config, handle, indent=2)
        return path

    def generate_config(self):
        """Write test_config.json from the packaged defaults, with fresh passwords and certs."""
        defaults = paths.data_dir() / TEST_CONFIG_DEFAULTS
        config = output.read_json_file(defaults, 'defaults file')

        for user in config.get('users', []):
            user['password'] = generate_password()

        for node in config.get('nodes', []):
            thing_name = node.get('thing_name', '')
            key_type = 'rsa' if 'rsa' in thing_name.lower() else 'ec'
            key_pem, cert_pem = generate_key_and_cert(thing_name, key_type)
            node['cert'] = cert_pem
            node['key'] = key_pem

        path = self.write_config(config)
        self._config = config
        output.ok(f"Wrote {path}: {len(config.get('users', []))} user(s), "
                  f"{len(config.get('nodes', []))} node(s), passwords and certs auto-generated. "
                  "Gitignored — never commit it.")
        return config

    # --- identities ---------------------------------------------------------

    def resolve_password(self, username):
        """Password for an identity test_config.json does not carry.

        Ordered so automation has a non-interactive route without pushing credentials through argv,
        where other processes and shell history can see them.
        """
        if self.password:
            return self.password
        if os.environ.get('RMNG_PASSWORD'):
            return os.environ['RMNG_PASSWORD']
        try:
            password = getpass.getpass(f"Password for {username}: ")
        except (EOFError, KeyboardInterrupt):
            output.plain()
            return None
        self._password_prompted = True
        return password

    def get_user(self, identity):
        """A User from test_config.json by index or name, else from the given credentials.

        Any account already provisioned in the deployment can be driven directly, so exercising an
        existing user or admin needs neither `test-data setup` nor an entry in test_config.json.
        """
        users = self.config.get('users', [])
        entry = None
        try:
            index = int(identity)
        except ValueError:
            entry = next((u for u in users if u.get('name') == identity), None)
        else:
            # A bare number is always an index; treating an out-of-range one as a username would
            # turn a typo into a password prompt.
            if not 0 <= index < len(users):
                output.fail(f"No user at index {index} in test_config.json "
                            f"({len(users)} configured)")
            entry = users[index]

        if entry is None:
            password = self.resolve_password(identity)
            if not password:
                output.fail(f"No password supplied for '{identity}'")
            entry = {'name': identity, 'password': password, 'admin': self.is_admin}

        username = entry.get('name')
        password = entry.get('password')
        is_admin = entry.get('admin', False)

        if not username:
            output.fail(f"User {identity} is missing 'name'")
        if not password:
            output.fail(f"{'Admin' if is_admin else 'End user'} {username} is missing 'password'")

        settings = self.settings
        common = (username, password, settings.region, settings.identity_pool_id,
                  settings.api_gateway_url, settings.user_api_gateway_url, settings.iot_endpoint)
        if is_admin:
            # Admins authenticate against the admin Cognito pool (USER_PASSWORD_AUTH).
            return User(*common, admin_user_pool_id=settings.admin_user_pool_id,
                        admin_client_id=settings.admin_client_id, is_admin=True)
        if '@' not in username:
            output.fail(f"End user {username} must have an email address as its name")
        return User(*common, end_user_pool_id=settings.end_user_pool_id)

    def get_node(self, identity):
        """A Device from test_config.json by index or thing name.

        Devices need a certificate and key, so unlike users they cannot be given on the command line.
        """
        nodes = self.config.get('nodes', [])
        entry = None
        try:
            index = int(identity)
        except ValueError:
            entry = next((n for n in nodes if n.get('thing_name') == identity), None)
        else:
            if 0 <= index < len(nodes):
                entry = nodes[index]
        if entry is None:
            output.fail(f"Device '{identity}' not found in test_config.json")

        missing = [k for k in ('thing_name', 'cert', 'key') if not entry.get(k)]
        if missing:
            output.fail(f"Device {identity} is missing {', '.join(missing)} in test_config.json")

        ca_cert = self.config.get('ca_cert')
        if not ca_cert:
            output.fail('test_config.json has no ca_cert')

        settings = self.settings
        return Device(entry['thing_name'], entry['key'], entry['cert'], ca_cert,
                      settings.iot_endpoint, settings.region, self.config.get('debug', False))


pass_session = click.make_pass_decorator(Session)


# A precondition token the SDK records -> the command that satisfies it. The SDK names the
# condition and nothing else; these tables are the only place that words the remedy.
USER_REMEDY = {
    'mqtt': 'connect',
}

DEVICE_REMEDY = {
    'mqtt': 'connect',
    'shadow': 'shadow-connect <name>',
    'group_info': 'group-info',
}


def explain(failure, blocked, remedies):
    """Add the precondition the subject blocked on to a command's own failure message.

    `blocked` is a NotReady or a NotReadyError: both name the condition and carry the token.

    @note The clauses are joined, never merged: the SDK words its own phrase, so nothing here may
    assume where it ends.
    """
    remedy = remedies.get(blocked.need)
    return f"{failure}: {blocked}; run `{remedy}` first." if remedy else f"{failure}: {blocked}"


def _pass_ready(f, resolve, remedies):
    """Give `f` the subject `resolve` returns, and name the precondition it failed on.

    Every command comes through one of these, so a command that failed on an unmet precondition
    names which one, and a new command needs no code of its own to do it.
    """
    @click.pass_context
    def wrapper(ctx, *args, **kwargs):
        subject = resolve(ctx.find_object(Session))
        # Scope the record to this command: in the REPL the subject outlives the line typed.
        subject.not_ready = None
        try:
            return ctx.invoke(f, subject, *args, **kwargs)
        except NotReadyError as e:
            # A precondition the SDK refuses to fail silently on, so the command reported nothing.
            output.fail(explain('Failed', e, remedies))
        except output.CommandError as e:
            if subject.not_ready is None:
                raise
            raise type(e)(explain(e.format_message(), subject.not_ready, remedies)) from None
    return click.decorators.update_wrapper(wrapper, f)


def pass_user(f):
    """Give a subcommand the User its `morpheus user <identity>` group resolved, signed in."""
    return _pass_ready(f, lambda session: session.authenticated_user(), USER_REMEDY)


def pass_unverified_user(f):
    """As pass_user, without signing in first. Only `auth` wants this: it provisions an account
    that may not exist, so it has to run when sign-in fails."""
    @click.pass_context
    def wrapper(ctx, *args, **kwargs):
        return ctx.invoke(f, ctx.find_object(Session).user, *args, **kwargs)
    return click.decorators.update_wrapper(wrapper, f)


def pass_device(f):
    """Give a subcommand the Device its `morpheus device <node>` group resolved."""
    return _pass_ready(f, lambda session: session.device, DEVICE_REMEDY)
