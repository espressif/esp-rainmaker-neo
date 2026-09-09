# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Offline tests for the command tree, the shell and the formatter.

Nothing here reaches AWS or a deployment: the point is that the surface — commands, flags, exit
codes, and the shell's error containment — holds without one.
"""

import json

import click
import pytest
from botocore.exceptions import ClientError
from click.testing import CliRunner

from esp_morpheus.cli import output, shell
from esp_morpheus.cli.context import Session, pass_device
from esp_morpheus.cli.main import cli, device, user
from esp_morpheus.outputs import OutputsError


@pytest.fixture
def run():
    runner = CliRunner()
    return lambda *args, **kwargs: runner.invoke(cli, list(args), **kwargs)


@pytest.fixture(autouse=True)
def plain_output():
    output.configure(json_mode=False, verbose=0)


# --- the tree ---------------------------------------------------------------

@pytest.mark.parametrize('args', [
    ['--help'],
    ['user', '--help'],
    ['device', '--help'],
    ['guide', '--help'],
    ['test-data', '--help'],
    ['bot-user', '--help'],
    ['gen-device', '--help'],
    ['user', 'someone', 'api', '--help'],
    ['user', 'someone', 'group', '--help'],
    ['user', 'someone', 'group', 'subgroup', '--help'],
    ['user', 'someone', 'matter', '--help'],
    ['user', 'someone', 'node', '--help'],
    ['user', 'someone', 'sharing', '--help'],
    ['user', 'someone', 'push', '--help'],
    ['device', 'node_light', 'subscribe', '--help'],
])
def test_help_needs_no_deployment(run, args):
    """Help renders everywhere without credentials or an outputs file."""
    result = run(*args)
    assert result.exit_code == 0, result.output


def test_unknown_command_is_a_usage_error(run):
    result = run('user', 'someone', 'no-such-command')
    assert result.exit_code == output.EXIT_USAGE


def test_version(run):
    assert run('--version').exit_code == 0


def test_admin_group_is_hidden_for_an_end_user(run):
    """The admin surface exists but stays out of an end user's help."""
    listing = run('user', 'someone@example.com', '--help').output
    assert 'admin' not in listing.split('Commands:')[1]


def test_admin_group_is_listed_for_an_admin(run):
    listing = run('user', '--admin', 'someone', '--help').output
    assert 'admin' in listing.split('Commands:')[1]


def test_a_group_option_after_the_identity_says_where_it_goes(run):
    """click stops parsing a group's options at its positional, so this would otherwise read as
    an unknown command and point at the wrong thing."""
    result = run('user', 'someone', '--admin', 'admin', 'platforms')
    assert result.exit_code == output.EXIT_USAGE
    assert 'goes before the identity' in result.output


def test_guide_runs_without_authenticating(run, monkeypatch, tmp_path):
    """`guide` prints console steps, so it must not need an identity or a password."""
    outputs = tmp_path / 'outputs.json'
    outputs.write_text(json.dumps({'rmng-base': {
        'StackRegion': 'us-east-1', 'StackAccountId': '1', 'IdentityPoolId': 'p',
        'ApiGatewayUrl': 'https://api', 'IoTEndpointUrl': 'iot', 'DefaultThingPolicyName': 'pol'}}))
    result = run('--client-outputs', str(outputs), 'guide', 'ios')
    assert result.exit_code == 0
    assert 'Apple Push Notifications' in result.output


def test_guide_rejects_an_unknown_topic(run):
    assert run('guide', 'nonsense').exit_code == output.EXIT_USAGE


# --- AWS credentials --------------------------------------------------------

OUTPUTS = {
    'rmng-base': {
        'StackRegion': 'ap-south-1', 'StackAccountId': '111122223333',
        'IdentityPoolId': 'ap-south-1:abc', 'ApiGatewayUrl': 'https://api.example.com',
        'IoTEndpointUrl': 'iot.example.com', 'DefaultThingPolicyName': 'DefaultThingPolicy',
    },
    'espuser-base': {'EspUserApiUrl': 'https://user.example.com/'},
}


@pytest.fixture
def outputs_file(tmp_path):
    path = tmp_path / 'rmng-outputs.json'
    path.write_text(json.dumps(OUTPUTS))
    return str(path)


def test_settings_load_without_touching_aws(outputs_file, monkeypatch):
    """Reading the outputs is file or HTTP I/O. Nothing about it needs a caller identity."""
    def explode(*args, **kwargs):
        raise AssertionError('verify_aws_identity must not run for a plain settings read')

    monkeypatch.setattr('esp_morpheus.cli.context.verify_aws_identity', explode)
    session = Session(outputs_source=outputs_file)
    assert session.settings.account_id == '111122223333'


def test_user_context_needs_no_aws_credentials(outputs_file, monkeypatch):
    """An end user signs in against the deployment's own API and is handed credentials by it, so
    requiring ambient AWS credentials locks out every caller who legitimately has none."""
    def explode(*args, **kwargs):
        raise AssertionError('the user path must not require AWS credentials')

    monkeypatch.setattr('esp_morpheus.cli.context.verify_aws_identity', explode)
    session = Session(outputs_source=outputs_file)
    session.password = 'secret'
    session.select_user('someone@example.com')
    assert session.user.username == 'someone@example.com'


def test_device_context_needs_no_aws_credentials(outputs_file, monkeypatch, tmp_path):
    """The device path is mutual-TLS MQTT, authenticated by the node's own certificate."""
    from esp_morpheus.cli import context as context_module

    def explode(*args, **kwargs):
        raise AssertionError('the device path must not require AWS credentials')

    monkeypatch.setattr(context_module, 'verify_aws_identity', explode)
    session = Session(outputs_source=outputs_file)
    session._config = {'ca_cert': 'ca', 'nodes': [
        {'thing_name': 'node_rsa', 'cert': 'cert', 'key': 'key'}]}
    session.select_device('node_rsa')
    assert session.device.node_thing_name == 'node_rsa'


def test_require_aws_verifies_once(outputs_file, monkeypatch):
    calls = []
    monkeypatch.setattr('esp_morpheus.cli.context.verify_aws_identity', calls.append)
    session = Session(outputs_source=outputs_file)
    session.require_aws()
    session.require_aws()
    assert len(calls) == 1


def test_an_unusable_deployment_fails_the_command_not_the_process(outputs_file, monkeypatch):
    """OutputsError is a SystemExit, which no `except Exception` catches, so an expired token or a
    mismatched account used to end the whole REPL instead of the one command."""
    def reject(settings):
        raise OutputsError('could not verify AWS identity via STS: Token has expired')

    monkeypatch.setattr('esp_morpheus.cli.context.verify_aws_identity', reject)
    session = Session(outputs_source=outputs_file)
    with pytest.raises(output.CommandError) as raised:
        session.require_aws()
    assert 'Token has expired' in raised.value.format_message()


def test_unreadable_outputs_fail_the_command_not_the_process(monkeypatch):
    monkeypatch.setattr('esp_morpheus.cli.context.RmngSettings.from_source',
                        lambda source: (_ for _ in ()).throw(OutputsError('missing StackRegion')))
    with pytest.raises(output.CommandError):
        Session(outputs_source='rmng-outputs.json').settings


def test_the_shell_survives_a_stray_process_exit():
    """A library that calls sys.exit must not take the session with it."""
    @click.command('boom')
    def boom():
        raise SystemExit('Error: something exited')

    group = click.Group('root', commands={'boom': boom})
    shell.dispatch(group, click.Context(group), ['boom'])


def test_there_is_no_way_to_skip_the_account_check(run):
    """The check only runs for commands that create, delete or sweep real resources, so an escape
    hatch would only ever weaken it where a wrong account is unrecoverable."""
    assert '--skip-account-check' not in run('--help').output
    assert run('--skip-account-check', 'test-data', 'setup').exit_code == output.EXIT_USAGE


# --- password verification --------------------------------------------------

class FakeUser:
    """A User that signs in only when handed the right password."""

    def __init__(self, correct='right', username='someone@example.com'):
        self.username = username
        self.password = 'wrong'
        self.correct = correct
        self.token = None
        self.attempts = []

    def get_cognito_token(self):
        self.attempts.append(self.password)
        self.token = 'a-token' if self.password == self.correct else None
        return self.token


@pytest.fixture
def signed_in(outputs_file, monkeypatch):
    def build(user, typed=()):
        session = Session(outputs_source=outputs_file)
        session._user = user
        session._user_identity = user.username
        session._password_prompted = bool(typed)
        typed = list(typed)
        monkeypatch.setattr('esp_morpheus.cli.context.getpass.getpass',
                            lambda prompt: typed.pop(0))
        return session
    return build


def test_a_correct_password_signs_in_once(signed_in):
    user = FakeUser()
    user.password = 'right'
    assert signed_in(user).authenticated_user() is user
    assert user.attempts == ['right']


def test_a_typed_password_can_be_retyped(signed_in):
    """A typo at the prompt is recoverable; the alternative is finding out after the shell opens."""
    user = FakeUser()
    session = signed_in(user, typed=['still-wrong', 'right'])
    assert session.authenticated_user() is user
    assert user.attempts == ['wrong', 'still-wrong', 'right']


def test_retries_are_bounded(signed_in):
    user = FakeUser(correct='never-typed')
    session = signed_in(user, typed=['nope', 'nope-again'])
    with pytest.raises(output.AuthError):
        session.authenticated_user()
    assert len(user.attempts) == 3


def test_a_password_from_a_script_is_not_re_prompted(signed_in):
    """--password and RMNG_PASSWORD come from something that cannot answer a prompt."""
    user = FakeUser(correct='never')
    session = signed_in(user)          # not prompted for
    with pytest.raises(output.AuthError) as caught:
        session.authenticated_user()
    assert caught.value.exit_code == output.EXIT_AUTH
    assert user.attempts == ['wrong']


def test_the_shell_warns_instead_of_aborting(signed_in):
    """An account that does not exist yet cannot sign in, and `auth` from the prompt creates it."""
    user = FakeUser(correct='never')
    session = signed_in(user)
    assert session.authenticated_user(on_failure='warn') is user


def test_an_already_signed_in_user_is_not_re_authenticated(signed_in):
    user = FakeUser()
    user.token = 'a-token'
    signed_in(user).authenticated_user()
    assert user.attempts == []


class NewAccount:
    """An account that does not exist until it is provisioned."""

    username = 'new@example.com'
    is_admin = False

    def __init__(self):
        self.exists = False
        self.provisioned = 0

    def get_aws_credentials(self):
        return self.exists

    def register_user_via_lambda(self, email=None):
        self.provisioned += 1
        self.exists = True


def test_auth_provisions_an_account_that_does_not_exist_yet():
    """The first sign-in for a new account necessarily fails, and provisioning is what creates it,
    so treating that failure as fatal left no way to make one."""
    from esp_morpheus.cli.user import auth

    user = NewAccount()
    auth.callback.__wrapped__(user)
    assert user.provisioned == 1 and user.exists


def test_auth_reports_an_account_it_could_not_sign_in(capsys):
    from esp_morpheus.cli.user import auth

    user = NewAccount()
    user.register_user_via_lambda = lambda email=None: None   # provisioning does not take
    with pytest.raises(output.AuthError) as caught:
        auth.callback.__wrapped__(user)
    assert caught.value.exit_code == output.EXIT_AUTH


def test_auth_does_not_pre_verify_the_password(monkeypatch):
    """`auth` must reach Session.user, not authenticated_user, or it can never create an account."""
    def explode(*args, **kwargs):
        raise AssertionError('auth must not require a working sign-in first')

    monkeypatch.setattr(Session, 'authenticated_user', explode)
    session = Session()
    session._user = NewAccount()
    session._user_identity = 'new@example.com'
    assert session.user.username == 'new@example.com'


# --- gen-device -------------------------------------------------------------

def test_gen_device_stdout_prints_the_material_and_writes_nothing(run, tmp_path, monkeypatch):
    monkeypatch.setenv('MORPHEUS_CONFIG_DIR', str(tmp_path))
    result = run('gen-device', 'node_test', 'ec', '--stdout')
    assert result.exit_code == 0
    assert 'BEGIN CERTIFICATE' in result.output
    assert not (tmp_path / 'test_config.json').exists()


def test_gen_device_appends_to_test_config(run, tmp_path, monkeypatch):
    """The point of the command: the node is usable as `morpheus device <index>` straight after."""
    monkeypatch.setenv('MORPHEUS_CONFIG_DIR', str(tmp_path))
    config = tmp_path / 'test_config.json'
    config.write_text(json.dumps({'nodes': [{'thing_name': 'node_ec', 'cert': 'c', 'key': 'k'}]}))

    assert run('gen-device', 'node_test', 'ec').exit_code == 0
    nodes = json.loads(config.read_text())['nodes']
    assert [n['thing_name'] for n in nodes] == ['node_ec', 'node_test']
    assert 'BEGIN CERTIFICATE' in nodes[1]['cert']


def test_gen_device_seeds_the_defaults_when_there_is_no_config(run, tmp_path, monkeypatch):
    """`test-data setup` writes the same defaults, so dead-ending the caller there would reach a
    larger side effect than doing it here."""
    monkeypatch.setenv('MORPHEUS_CONFIG_DIR', str(tmp_path))
    assert run('gen-device', 'node_test', 'ec').exit_code == 0
    config = json.loads((tmp_path / 'test_config.json').read_text())
    assert config['users'] and config['ca_cert']
    assert config['nodes'][-1]['thing_name'] == 'node_test'


def test_gen_device_refuses_to_overwrite_without_force(run, tmp_path, monkeypatch):
    monkeypatch.setenv('MORPHEUS_CONFIG_DIR', str(tmp_path))
    config = tmp_path / 'test_config.json'
    config.write_text(json.dumps({'nodes': [{'thing_name': 'node_test', 'cert': 'c', 'key': 'k'}]}))

    result = run('gen-device', 'node_test', 'ec')
    assert result.exit_code == output.EXIT_FAILURE
    assert json.loads(config.read_text())['nodes'][0]['cert'] == 'c'


def test_gen_device_force_keeps_the_other_node_fields(run, tmp_path, monkeypatch):
    """A regenerated certificate must not drop the association the entry already carried."""
    monkeypatch.setenv('MORPHEUS_CONFIG_DIR', str(tmp_path))
    config = tmp_path / 'test_config.json'
    config.write_text(json.dumps({'nodes': [
        {'thing_name': 'node_test', 'cert': 'c', 'key': 'k',
         'associate_to': 'test-user1@example.com', 'node_cfg': 'node_config_va_multi.json'}]}))

    assert run('gen-device', 'node_test', 'ec', '--force').exit_code == 0
    entry = json.loads(config.read_text())['nodes'][0]
    assert 'BEGIN CERTIFICATE' in entry['cert']
    assert entry['associate_to'] == 'test-user1@example.com'
    assert entry['node_cfg'] == 'node_config_va_multi.json'


def test_gen_device_rejects_a_bad_key_type(run):
    assert run('gen-device', 'node_test', 'dsa').exit_code == output.EXIT_USAGE


# --- bot-user ---------------------------------------------------------------

class FakeIam:
    """The IAM surface `bot-user` uses, recording every call."""

    def __init__(self, existing=()):
        self.users = {name: {'keys': ['AKIAOLD'], 'policies': []} for name in existing}
        self.calls = []
        self._next_key = 0

    def _record(self, name, **kwargs):
        self.calls.append((name, kwargs))

    def get_user(self, UserName):
        if UserName not in self.users:
            raise ClientError({'Error': {'Code': 'NoSuchEntity'}}, 'GetUser')
        return {'User': {'UserName': UserName}}

    def create_user(self, UserName, Tags=None):
        self._record('create_user', UserName=UserName)
        self.users[UserName] = {'keys': [], 'policies': []}

    def attach_user_policy(self, UserName, PolicyArn):
        self._record('attach_user_policy', UserName=UserName)
        self.users[UserName]['policies'].append(PolicyArn)

    def detach_user_policy(self, UserName, PolicyArn):
        self._record('detach_user_policy', UserName=UserName)
        if PolicyArn not in self.users[UserName]['policies']:
            raise ClientError({'Error': {'Code': 'NoSuchEntity'}}, 'DetachUserPolicy')
        self.users[UserName]['policies'].remove(PolicyArn)

    def list_attached_user_policies(self, UserName):
        return {'AttachedPolicies': [{'PolicyArn': arn} for arn in self.users[UserName]['policies']]}

    def create_access_key(self, UserName):
        self._next_key += 1
        key_id = f"AKIANEW{self._next_key}"
        self.users[UserName]['keys'].append(key_id)
        return {'AccessKey': {'AccessKeyId': key_id, 'SecretAccessKey': 'secret'}}

    def delete_access_key(self, UserName, AccessKeyId):
        self._record('delete_access_key', UserName=UserName, AccessKeyId=AccessKeyId)
        self.users[UserName]['keys'].remove(AccessKeyId)

    def delete_user(self, UserName):
        self._record('delete_user', UserName=UserName)
        del self.users[UserName]

    def get_paginator(self, operation):
        assert operation == 'list_access_keys'
        outer = self

        class Paginator:
            def paginate(self, UserName):
                keys = list(outer.users[UserName]['keys'])
                return [{'AccessKeyMetadata': [{'AccessKeyId': k} for k in keys]}]

        return Paginator()


@pytest.fixture
def bot(tmp_path, monkeypatch, outputs_file):
    """`bot-user <args>` against a fake IAM, with test_config.json and the keys under tmp_path."""
    from esp_morpheus.cli import testdata

    monkeypatch.setenv('MORPHEUS_CONFIG_DIR', str(tmp_path))
    monkeypatch.setattr('esp_morpheus.cli.context.verify_aws_identity', lambda settings: None)
    iam = FakeIam()
    monkeypatch.setattr(testdata.boto3, 'client', lambda service, **kwargs: iam)

    runner = CliRunner()

    def invoke(*args):
        return runner.invoke(cli, ['--client-outputs', outputs_file, 'bot-user', *args])

    invoke.iam = iam
    invoke.config = tmp_path / 'test_config.json'
    invoke.credentials = tmp_path / 'bot-iam-user-credentials.json'
    return invoke


def test_bot_user_create_needs_no_prepared_config(bot):
    """The whole point: a fresh checkout has no ci_bot_user key, and create still works."""
    assert not bot.config.exists()
    result = bot('create')
    assert result.exit_code == 0, result.output
    assert 'bot' in bot.iam.users
    assert json.loads(bot.credentials.read_text())['user_name'] == 'bot'


def test_bot_user_create_remembers_the_name_it_used(bot):
    assert bot('create', 'rmng-ci').exit_code == 0
    assert json.loads(bot.config.read_text())['ci_bot_user'] == 'rmng-ci'
    assert 'rmng-ci' in bot.iam.users


def test_bot_user_create_reads_the_remembered_name(bot):
    bot.config.write_text(json.dumps({'ci_bot_user': 'rmng-ci'}))
    assert bot('create').exit_code == 0
    assert 'rmng-ci' in bot.iam.users


def test_bot_user_create_refuses_an_existing_user(bot):
    """The user's active key must not be invalidated by a repeated create."""
    assert bot('create').exit_code == 0
    result = bot('create')
    assert result.exit_code == output.EXIT_FAILURE
    assert '--rotate' in result.output
    assert bot.iam.users['bot']['keys'] == ['AKIANEW1']


def test_bot_user_rotate_replaces_the_key(bot):
    """The recovery path when the credentials file is lost: IAM allows two keys per user."""
    assert bot('create').exit_code == 0
    bot.credentials.unlink()
    assert bot('create', '--rotate').exit_code == 0
    assert bot.iam.users['bot']['keys'] == ['AKIANEW2']
    assert json.loads(bot.credentials.read_text())['access_key_id'] == 'AKIANEW2'


def test_bot_user_create_refuses_a_second_name(bot):
    """One credentials file, so a second name would strand the first user's live admin key."""
    assert bot('create', 'alpha').exit_code == 0
    result = bot('create', 'beta')
    assert result.exit_code == output.EXIT_FAILURE
    assert 'alpha' in result.output
    assert 'beta' not in bot.iam.users
    assert json.loads(bot.credentials.read_text())['user_name'] == 'alpha'


def test_bot_user_create_replaces_a_name_iam_no_longer_has(bot):
    """A tracked user deleted out of band is a stale record, not a reason to dead-end."""
    assert bot('create', 'alpha').exit_code == 0
    del bot.iam.users['alpha']
    assert bot('create', 'beta').exit_code == 0
    assert json.loads(bot.credentials.read_text())['user_name'] == 'beta'
    assert json.loads(bot.config.read_text())['ci_bot_user'] == 'beta'


def test_bot_user_create_tracks_the_credentials_file_over_the_config(bot):
    """The file holds the key a create would overwrite, so it names the bot that is really tracked."""
    bot.config.write_text(json.dumps({'ci_bot_user': 'beta'}))
    bot.credentials.write_text(json.dumps({'user_name': 'alpha', 'access_key_id': 'AKIAOLD',
                                           'secret_access_key': 'secret'}))
    bot.iam.users['alpha'] = {'keys': ['AKIAOLD'], 'policies': []}

    result = bot('create')
    assert result.exit_code == output.EXIT_FAILURE
    assert 'alpha' in result.output


def test_bot_user_delete_keeps_another_bots_credentials(bot):
    assert bot('create', 'alpha').exit_code == 0
    bot.iam.users['beta'] = {'keys': [], 'policies': []}

    assert bot('delete', 'beta').exit_code == 0
    assert 'beta' not in bot.iam.users
    assert json.loads(bot.credentials.read_text())['user_name'] == 'alpha'


def test_bot_user_delete_clears_the_remembered_name(bot):
    """A record pointing at a deleted user would make the next create report the wrong conflict."""
    assert bot('create', 'alpha').exit_code == 0
    assert bot('delete').exit_code == 0
    assert 'ci_bot_user' not in json.loads(bot.config.read_text())
    assert bot('create', 'beta').exit_code == 0


def test_bot_user_show_names_the_owner_of_the_keys(bot):
    assert bot('create', 'alpha').exit_code == 0
    result = bot('show', 'beta')
    assert result.exit_code == 0
    assert 'credentials_belong_to' in result.output
    assert 'alpha' in result.output


def test_bot_user_delete_uses_the_remembered_name(bot):
    assert bot('create', 'rmng-ci').exit_code == 0
    assert bot('delete').exit_code == 0
    assert bot.iam.users == {}
    assert not bot.credentials.exists()


def test_bot_user_delete_removes_a_stale_credentials_file(bot):
    bot.credentials.write_text('{}')
    assert bot('delete').exit_code == 0
    assert not bot.credentials.exists()


def test_bot_user_show_reports_state_and_creates_nothing(bot):
    result = bot('show')
    assert result.exit_code == 0
    assert 'bot' in result.output
    assert bot.iam.users == {}
    assert not bot.config.exists()


# --- simulators ---------------------------------------------------------------

def test_app_sim_drives_an_identity_outside_test_config(outputs_file, tmp_path, monkeypatch):
    """test_config.json is read only to look an identity up, so a resolved user needs none — the
    simulator can drive any account in the deployment, like `morpheus user` can."""
    from esp_morpheus.sims.app import AppSim

    monkeypatch.setenv('MORPHEUS_CONFIG_DIR', str(tmp_path))   # empty: no test_config.json
    session = Session(outputs_source=outputs_file)
    session.password = 'typed-at-the-prompt'
    session.select_user('someone@example.com')

    sim = AppSim('someone@example.com', user=session.user, rmng_outputs_path=outputs_file)
    assert sim.user.username == 'someone@example.com'
    assert sim.config == {}
    assert not (tmp_path / 'test_config.json').exists()


def test_app_sim_still_reads_test_config_when_given_no_user(outputs_file, tmp_path):
    """The pre-existing lookup stays for callers that pass only an id."""
    from esp_morpheus.sims.app import AppSim

    config = tmp_path / 'test_config.json'
    config.write_text(json.dumps({'users': [{'name': 'seeded@example.com', 'password': 'pw'}]}))
    sim = AppSim('seeded@example.com', config_path=str(config), rmng_outputs_path=outputs_file)
    assert sim.user.username == 'seeded@example.com'


def test_app_sim_takes_the_same_password_options_as_user(run):
    listing = run('app-sim', '--help').output
    assert '--password' in listing and '--admin' in listing


# --- preconditions ----------------------------------------------------------

def _unready_node(**ready):
    """A Device with every precondition unmet, past __init__ so it needs no MQTT stack."""
    from esp_morpheus.sdk.device import Device

    node = Device.__new__(Device)
    node.mqtt_connection = node.shadow_client = node.group_id = None
    node.node_key = node.node_cert = node.node_thing_name = None
    node.not_ready = None
    for name, value in ready.items():
        setattr(node, name, value)
    return node


def _device_ctx(node):
    ctx = click.Context(device, obj=Session())
    ctx.obj.select_device('node_light')
    ctx.obj._device = node
    return ctx


# Every guarded method that reports a refusal through its return value, with that value.
@pytest.mark.parametrize('method, blocked, args', [
    ('sign_challenge', None, ('challenge',)),
    ('sign_matter_attestation', None, (b'nocsr', b'challenge')),
    ('update_named_shadow', False, ('local', {})),
    ('update_shadow', False, ('{}',)),
    ('destroy_test_node', False, ()),
    ('register_test_node', False, ()),
    ('publish_to_cloud', False, ({},)),
    ('send_direct_notification', False, ({},)),
    ('get_group_info', None, ()),
    ('get_schedule_version', None, ()),
    ('get_schedule_details', None, ()),
    ('publish_timeseries_data', False, ('k', 'int', 1)),
    ('publish_timeseries_batch', False, ([{'k': 'a'}],)),
    ('set_node_config', False, ({},)),
    ('subscribe', False, ()),
    ('unsubscribe', False, ('topic',)),
    ('get_trigger_version', None, ()),
    ('get_trigger_details', None, ()),
])
def test_a_blocked_call_returns_what_it_always_returned(method, blocked, args):
    """The guard reports the condition without changing any caller's contract."""
    node = _unready_node()
    assert getattr(node, method)(*args) is blocked
    assert node.not_ready is not None


def test_a_refusal_is_never_silent_by_default():
    """A method that states no return value raises, so a caller cannot miss the refusal."""
    from esp_morpheus.sdk.errors import NotReadyError, requires

    class Probe:
        node_thing_name = None
        not_ready = None
        not_ready_subject = 'node'
        not_ready_log = staticmethod(print)

        @requires('node_id')
        def act(self):
            return 'ran'

    probe = Probe()
    with pytest.raises(NotReadyError) as raised:
        probe.act()
    assert raised.value.need == 'node_id'
    assert str(raised.value) == 'the node has no thing name'
    assert probe.not_ready.need == 'node_id'


def test_the_guard_does_not_touch_an_exception_from_the_method():
    """The guard checks preconditions and then stands aside."""
    from esp_morpheus.sdk.errors import requires

    class Probe:
        mqtt_connection = None
        not_ready = None

        @requires('mqtt', blocked=False)
        def boom(self):
            raise RuntimeError('from the body')

    probe = Probe()
    assert probe.boom() is False

    probe.mqtt_connection = object()
    with pytest.raises(RuntimeError, match='from the body'):
        probe.boom()
    assert probe.not_ready is None


def test_the_first_unmet_precondition_is_the_one_reported():
    """A node with neither connection nor group info needs connecting before grouping."""
    node = _unready_node()
    node.send_direct_notification({})
    assert node.not_ready.need == 'mqtt'

    node.mqtt_connection = object()
    node.send_direct_notification({})
    assert node.not_ready.need == 'group_info'


def test_a_call_that_runs_clears_an_earlier_reason():
    node = _unready_node()
    assert node.get_group_info() is None
    node.mqtt_connection = object()
    node.subscribe(topic='rainmaker/nodes/node_light/from_cloud')
    assert node.not_ready is None


def test_the_shadow_branch_reports_the_shadow_it_needs():
    """`subscribe --shadow` needs more than the connection the decorator can check."""
    node = _unready_node(mqtt_connection=object(), node_thing_name='node_light')
    assert node.subscribe(shadow_name='local') is False
    assert node.not_ready.need == 'shadow'


def test_a_command_names_the_precondition_and_the_remedy(capsys):
    node = _unready_node(mqtt_connection=object(), node_thing_name='node_light')
    ctx = _device_ctx(node)
    shell.dispatch(device, ctx, ['direct-notify', '{"push":true}'])
    message = capsys.readouterr().out
    assert 'the node has no group info' in message
    assert 'run `group-info` first' in message


def test_a_refused_call_is_not_read_as_an_answer(capsys):
    """`group-info` may not conclude the node has no group when it never got to ask."""
    node = _unready_node()
    shell.dispatch(device, _device_ctx(node), ['group-info'])
    message = capsys.readouterr().out
    assert 'Failed to fetch the group info' in message
    assert 'run `connect` first' in message
    assert 'not associated with any group' not in message


def test_a_failure_of_its_own_is_not_blamed_on_a_precondition(capsys):
    """A reason recorded by an earlier command may not leak into the next one's message."""
    node = _unready_node(mqtt_connection=object(), node_thing_name='node_light')
    ctx = _device_ctx(node)
    shell.dispatch(device, ctx, ['direct-notify', '{"push":true}'])
    capsys.readouterr()

    shell.dispatch(device, ctx, ['publish', 'local', 'not-json'])
    message = capsys.readouterr().out
    assert 'Invalid JSON' in message
    assert 'group info' not in message


def test_a_raised_precondition_reaches_the_shell_with_its_remedy(capsys):
    """A command that refuses by raising names the condition and the remedy, like one that
    refuses by returning."""
    from esp_morpheus.sdk.errors import requires

    @click.command('probe')
    @pass_device
    def probe(node):
        requires('node_id')(lambda self: None)(node)

    group = click.Group('device', commands={'probe': probe})
    node = _unready_node(group_id=None)
    ctx = _device_ctx(node)
    shell.dispatch(group, ctx, ['probe'])
    message = capsys.readouterr().out
    assert 'the node has no thing name' in message
    assert 'NotReadyError' not in message


def test_connect_prints_what_the_cloud_sends_back(capsys):
    """A reply on from_cloud is a result, so `connect` registers a printer for it."""
    import json as _json
    from queue import Queue

    node = _unready_node(mqtt_connection=object(), node_thing_name='node_light')
    node.from_cloud_queue = Queue()
    node.callbacks = {'from_cloud': None, 'params': None, 'shadow': None}
    node.connect = lambda: True

    ctx = _device_ctx(node)
    shell.dispatch(device, ctx, ['connect'])
    assert node.callbacks['from_cloud'] is output.inbound
    capsys.readouterr()

    node.on_message_received('rainmaker/nodes/node_light/from_cloud',
                             _json.dumps({'getGroupInfo': {'pgrp': 'grp-1'}}).encode())
    shown = capsys.readouterr().out
    assert 'rainmaker/nodes/node_light/from_cloud' in shown
    assert 'grp-1' in shown


def test_a_protocol_event_does_not_wait_for_v(capsys):
    """What the cloud told the node happened, so it shows; what the node is doing does not."""
    from esp_morpheus.sdk import device as sdk

    sdk.device_event('[local][v4][reported] update accepted {"Power": true}')
    assert 'update accepted' in capsys.readouterr().out

    sdk.device_log('Subscribing to topic: rainmaker/nodes/node_light/from_cloud')
    assert capsys.readouterr().out == ''


def test_a_message_prints_where_it_arrived(capsys):
    """Inline and in order: a message is not held until the command that it landed during ends."""
    output.inbound('rainmaker/nodes/node_light/params-g1/params', {'Power': True})
    output.ok('the command finished')
    shown = capsys.readouterr().out
    assert shown.index('Power') < shown.index('the command finished')


def test_a_record_is_never_cut_open_by_a_message():
    """The two orders interleave; they must not split. A message delivered from another thread
    part-way through a record has to wait for the record, and no longer."""
    import threading
    from rich.console import Console

    arrived = []

    class InterruptingFile:
        """A terminal that lets a subscription deliver in the middle of a record."""

        def __init__(self):
            self.lines = []
            self.intruder = None

        def write(self, text):
            self.lines.append(text)
            if self.intruder is None:
                self.intruder = threading.Thread(target=output.inbound, args=(
                    'rainmaker/nodes/node_light/params-g1/params', {'Power': True}))
                self.intruder.start()
                arrived.append(True)

        def flush(self):
            pass

        def isatty(self):
            return False

    terminal = InterruptingFile()
    output._state.out = output._state.msg = Console(file=terminal, soft_wrap=True, width=100)
    output.emit_kv('Group info', {'group_id': 'gzncge', 'subgroup_ids': ['a', 'b']})
    terminal.intruder.join(timeout=5)
    assert arrived, 'the subscription never got a chance to interrupt'

    text = ''.join(terminal.lines)
    record_ends = text.index('gzncge') + len('gzncge')
    assert text.index('Power') > record_ends, 'the message landed inside the record'


def test_a_nested_block_is_allowed():
    """A helper that takes the lock may be called from a caller that already holds it."""
    with output.block():
        with output.block():
            output.ok('nested')


def test_an_idle_message_prints_in_one_write():
    """The prompt is redrawn after every write, so a split block would have one drawn through
    the middle of it."""
    class CountingFile:
        def __init__(self):
            self.writes = []

        def write(self, text):
            self.writes.append(text)

        def flush(self):
            pass

        def isatty(self):
            return False

    from rich.console import Console

    counted = CountingFile()
    output._state.msg = Console(file=counted, soft_wrap=True)
    output.inbound('rainmaker/nodes/node_light/from_cloud', {'getGroupInfo': {'pgrp': 'g1'}})
    body = ''.join(counted.writes)
    assert len([w for w in counted.writes if w.strip()]) == 1
    assert 'rainmaker/nodes/node_light/from_cloud' in body
    assert 'getGroupInfo' in body


def test_an_inbound_message_is_not_a_payload(capsys):
    """Under --json a subscription delivers whenever it likes, so it must stay off stdout."""
    output.configure(json_mode=True)
    output.inbound('rainmaker/nodes/node_light/from_cloud', {'getGroupInfo': {'pgrp': 'grp-1'}})
    output.emit_json({'group_id': 'grp-1'})
    captured = capsys.readouterr()
    assert 'getGroupInfo' in captured.err
    assert 'getGroupInfo' not in captured.out


def test_the_device_trace_is_quiet_until_v(capsys):
    from esp_morpheus.sdk import device as sdk

    output.configure(verbose=0)
    sdk.device_log('Subscribing to topic: rainmaker/nodes/node_light/from_cloud')
    assert capsys.readouterr().out == ''

    output.configure(verbose=1)
    sdk.device_log('[local][v3][reported] updated to {"x": 1}')
    # Printed literally: rich would read `[reported]` as a style and fail on it.
    assert '[local][v3][reported]' in capsys.readouterr().out


# --- the shell --------------------------------------------------------------

def _shell_ctx(group=user):
    ctx = click.Context(group, obj=Session())
    ctx.obj.select_user('someone')
    return ctx


def test_split_line_keeps_a_bare_json_payload():
    """Typing a payload without quotes is how the shell has always taken one."""
    assert shell.split_line('direct-notify {"push":true}') == ['direct-notify', '{"push":true}']
    assert shell.split_line('set-params {"Light": {"Power": true}}') == [
        'set-params', '{"Light": {"Power": true}}']
    assert shell.split_line('send [1, 2]') == ['send', '[1, 2]']


def test_split_line_still_splits_like_a_shell():
    assert shell.split_line("notify '{\"push\":true}'") == ['notify', '{"push":true}']
    assert shell.split_line('group create "my group"') == ['group', 'create', 'my group']
    assert shell.split_line('set-params --file "a {b}.json"') == [
        'set-params', '--file', 'a {b}.json']
    assert shell.split_line('   ') == []


def test_completion_tree_follows_the_command_tree():
    with _shell_ctx() as ctx:
        tree = shell._completion_tree(user, ctx)
    assert 'group' in tree and 'create' in tree['group']
    assert 'subgroup' in tree['group'] and 'add-node' in tree['group']['subgroup']
    # The shell's own words belong only at the top level.
    assert {'help', 'q', 'quit'} <= set(tree)
    assert 'help' not in tree['group']


def test_completion_tree_hides_admin_from_an_end_user():
    with _shell_ctx() as ctx:
        assert 'admin' not in shell._completion_tree(user, ctx)


def test_device_completion_tree():
    ctx = click.Context(device, obj=Session())
    with ctx:
        tree = shell._completion_tree(device, ctx)
    assert {'connect', 'subscribe', 'to-cloud', 'group-info', 'set-node-config'} <= set(tree)


def test_dispatch_contains_a_usage_error():
    """A bad command must return, not exit — otherwise one typo ends the session."""
    with _shell_ctx() as ctx:
        shell.dispatch(user, ctx, ['group', 'create'])       # missing NAME
        shell.dispatch(user, ctx, ['not-a-command'])
        shell.dispatch(user, ctx, ['group', 'not-a-thing'])


def test_dispatch_contains_a_raised_exception(monkeypatch):
    @click.command('boom')
    def boom():
        raise RuntimeError('the deployment fell over')

    with _shell_ctx() as ctx:
        user.add_command(boom)
        try:
            shell.dispatch(user, ctx, ['boom'])
        finally:
            user.commands.pop('boom')


def test_dispatch_contains_a_command_failure():
    @click.command('nope')
    def nope():
        output.fail('could not do the thing')

    with _shell_ctx() as ctx:
        user.add_command(nope)
        try:
            shell.dispatch(user, ctx, ['nope'])
        finally:
            user.commands.pop('nope')


def test_help_for_a_nested_command_does_not_raise(capsys):
    with _shell_ctx() as ctx:
        shell._print_help(user, ctx, [])
        shell._print_help(user, ctx, ['group'])
        shell._print_help(user, ctx, ['group', 'subgroup', 'add-node'])
        shell._print_help(user, ctx, ['not-a-command'])


# --- the formatter ----------------------------------------------------------

class FakeResponse:
    def __init__(self, status_code, payload=None, text=None, reason='OK'):
        self.status_code = status_code
        self.reason = reason
        self._payload = payload
        self.text = text if text is not None else (json.dumps(payload) if payload else '')

    def json(self):
        if self._payload is None:
            raise ValueError('no json')
        return self._payload


def test_emit_response_returns_the_body_on_success():
    assert output.emit_response(FakeResponse(200, {'nodes': []})) == {'nodes': []}


def test_emit_response_fails_on_4xx():
    with pytest.raises(output.CommandError) as caught:
        output.emit_response(FakeResponse(400, {'description': 'bad group id'}))
    assert caught.value.exit_code == output.EXIT_FAILURE
    assert 'bad group id' in str(caught.value)


def test_emit_response_reports_401_as_an_auth_failure():
    """A script retries credentials on 3 and gives up on 1, so the codes must differ."""
    with pytest.raises(output.AuthError) as caught:
        output.emit_response(FakeResponse(403, {'description': 'forbidden'}))
    assert caught.value.exit_code == output.EXIT_AUTH


def test_emit_response_accepts_an_empty_204():
    assert output.emit_response(FakeResponse(204, text=''), 'deleted') is None


def test_emit_response_handles_a_non_json_body():
    with pytest.raises(output.CommandError):
        output.emit_response(FakeResponse(502, text='<html>gateway</html>'))


def test_json_mode_puts_only_the_payload_on_stdout(capsys):
    output.configure(json_mode=True)
    output.ok('this is progress, not payload')
    output.emit_json({'nodes': ['a']})
    captured = capsys.readouterr()
    assert json.loads(captured.out) == {'nodes': ['a']}
    assert 'progress' in captured.err


def test_json_mode_keeps_an_sdk_print_off_stdout(capsys):
    """The SDK prints progress and protocol errors on stdout itself, which would corrupt the
    payload a script pipes into jq."""
    output.configure(json_mode=True)
    print('[CORS ERROR] preflight failed')
    output.emit_json({'nodes': []})
    captured = capsys.readouterr()
    assert json.loads(captured.out) == {'nodes': []}
    assert 'CORS ERROR' in captured.err


def test_configure_restores_stdout(capsys):
    output.configure(json_mode=True)
    output.configure(json_mode=False)
    print('back on stdout')
    assert 'back on stdout' in capsys.readouterr().out


def test_the_request_trace_is_off_by_default():
    """The trace prints bearer tokens and STS session credentials verbatim, so it is opt-in."""
    from esp_morpheus.sdk import user as user_sdk

    output.configure()
    assert user_sdk.request_logging is False
    output.configure(verbose=1)
    assert user_sdk.request_logging is True
    output.configure()


PEM = ('-----BEGIN CERTIFICATE-----\n'
       'MIIB4jCCAYigAwIBAgIJANIUpyPq89ZYMAoGCCqGSM49BAMCMEQxIDAeBgorBgEE\n'
       '-----END CERTIFICATE-----')


def test_a_multiline_value_is_copyable(capsys):
    """A PEM has to paste into a file or openssl unchanged, so no line may carry an indent."""
    output.emit_kv(None, {'short': 'x', 'Root CA': PEM})
    printed = capsys.readouterr().out
    assert '  Root CA:' in printed
    assert PEM in printed
    for line in PEM.splitlines():
        assert f'\n{line}\n' in printed, f'{line!r} is indented'


def test_a_list_value_is_one_item_per_line_in_the_value_column(capsys):
    """A list of ids is a field, not a block: it stays in the value column, unlike a PEM."""
    output.emit_kv(None, {'group_id': 'azkjt2', 'nodes': ['node_multi', 'node_switch']})
    lines = capsys.readouterr().out.splitlines()
    column = lines[0].index('azkjt2')
    assert lines[1].index('node_multi') == column
    assert lines[2].index('node_switch') == column
    assert lines[2].strip() == 'node_switch'      # no repeated key


def test_a_list_value_stays_an_array_in_the_payload(capsys):
    output.configure(raw=True)
    output.emit_kv(None, {'nodes': ['node_multi', 'node_switch']})
    assert json.loads(capsys.readouterr().out)['nodes'] == ['node_multi', 'node_switch']


def test_single_line_values_stay_in_the_key_column(capsys):
    output.emit_kv(None, {'group_id': 'azkjt2', 'Fabric ID': '617A6B6A74320000'}, min_width=14)
    first, second = capsys.readouterr().out.splitlines()[:2]
    assert first.index('azkjt2') == second.index('617A6B6A74320000')


GROUP_WITH_SUBGROUPS = {
    'group_id': 'azkjt2', 'group_name': 'My Home', 'access_type': 'primary',
    'node_ids': ['node_multi'],
    'subgroups': [{'subgroup_id': 'sg7f2a', 'subgroup_name': 'Kitchen', 'node_ids': ['node_multi']},
                  {'subgroup_id': 'sg91bc', 'subgroup_name': 'Hallway', 'node_ids': []}],
}


def test_group_blocks_share_one_key_column(capsys):
    """A plain group and a Matter one sit in the same listing, so their values must line up."""
    from esp_morpheus.cli.groups import emit_group

    emit_group(GROUP_WITH_SUBGROUPS)
    emit_group({'group_id': 'zfqm28'})
    lines = capsys.readouterr().out.splitlines()
    assert lines[0].index('azkjt2') == lines[-1].index('zfqm28')


def test_a_group_listing_shows_its_subgroup_ids(capsys):
    """Every other subgroup command takes a subgroup_id, so the listing has to yield one."""
    from esp_morpheus.cli.groups import emit_group

    emit_group(GROUP_WITH_SUBGROUPS)
    lines = capsys.readouterr().out.splitlines()
    printed = '\n'.join(lines)
    assert 'sg7f2a' in printed and 'Kitchen' in printed
    assert 'sg91bc' in printed and 'Hallway' in printed
    # One subgroup per line, aligned with the values above it.
    kitchen = next(line for line in lines if 'Kitchen' in line)
    hallway = next(line for line in lines if 'Hallway' in line)
    assert kitchen.index('Kitchen') == hallway.index('Hallway')
    assert hallway.strip().startswith('Hallway')


def test_a_group_listing_names_the_group(capsys):
    from esp_morpheus.cli.groups import emit_group

    emit_group(GROUP_WITH_SUBGROUPS)
    assert 'My Home' in capsys.readouterr().out



def test_the_matter_marker_stays_out_of_the_payload(capsys):
    """`--raw` has to report a group_id something else can use, not one with a label glued on."""
    from esp_morpheus.cli.groups import emit_group

    output.configure(raw=True)
    emit_group({'group_id': 'azkjt2', 'matter': {'fabric_id': '617A6B6A74320000'}})
    payload = json.loads(capsys.readouterr().out)
    assert payload['group_id'] == 'azkjt2'
    assert payload['matter'] == {'fabric_id': '617A6B6A74320000'}


def test_raw_prints_the_payload_as_json(capsys):
    output.configure(raw=True)
    output.emit_kv('Ignored title', {'group_id': 'ge5aq6'})
    assert json.loads(capsys.readouterr().out) == {'group_id': 'ge5aq6'}


def test_a_piped_payload_carries_no_colour(capsys, monkeypatch):
    """FORCE_COLOR asks rich to colour a pipe, which writes escape codes into JSON something is
    about to parse."""
    monkeypatch.setenv('FORCE_COLOR', '3')
    output.configure(raw=True)
    output.emit_kv(None, {'group_id': 'ge5aq6'})
    assert json.loads(capsys.readouterr().out) == {'group_id': 'ge5aq6'}


def test_raw_keeps_messages_on_stdout(capsys):
    """--raw changes the layout only. --json is the one that reserves stdout."""
    output.configure(raw=True)
    output.ok('progress')
    captured = capsys.readouterr()
    assert 'progress' in captured.out and captured.err == ''


def test_json_implies_raw():
    output.configure(json_mode=True)
    assert output.structured() and output.json_mode()
    output.configure(raw=True)
    assert output.structured() and not output.json_mode()
    output.configure()
    assert not output.structured()


def test_parse_json_arg_rejects_bad_json():
    with pytest.raises(output.CommandError):
        output.parse_json_arg('{not json')


def test_parse_json_arg_reads_a_file(tmp_path):
    path = tmp_path / 'payload.json'
    path.write_text('{"a": 1}')
    assert output.parse_json_arg('', str(path)) == {'a': 1}


def test_read_json_file_reports_a_missing_file():
    with pytest.raises(output.CommandError):
        output.read_json_file('/nonexistent/payload.json')
