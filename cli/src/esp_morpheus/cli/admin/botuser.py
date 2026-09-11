# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus admin bot-user ...` — the CI IAM user and its access keys."""

import json

import boto3
import click
from botocore.exceptions import ClientError

from ... import paths
from .. import output
from ..context import pass_session

ADMINISTRATOR_ACCESS_POLICY_ARN = 'arn:aws:iam::aws:policy/AdministratorAccess'

# Name of the IAM user when nothing names one. The key holding a chosen name is written by
# `bot-user create`, so no entry has to exist before the first run.
DEFAULT_BOT_USER_NAME = 'bot'
BOT_USER_CONFIG_KEY = 'ci_bot_user'


@click.group('bot-user')
def bot_user():
    """Create and delete the CI IAM bot user.

    One bot at a time: there is a single credentials file, so a second user under another name would
    overwrite the keys of the first and leave it in IAM with nothing tracking its access key.
    """


def _credentials_owner():
    """The user named by the credentials file, or None when there is no file and no usable name."""
    path = paths.bot_credentials_path()
    if not path.is_file():
        return None
    try:
        with open(path, 'r', encoding='utf-8') as handle:
            return json.load(handle).get('user_name') or None
    except (OSError, ValueError):
        return None


def _tracked_bot_user_name(session):
    """The bot this checkout already holds keys for, or None.

    The credentials file answers first: it is the record that a create would overwrite.
    """
    return _credentials_owner() or session.config.get(BOT_USER_CONFIG_KEY) or None


def _bot_user_name(session, name=None):
    return name or _tracked_bot_user_name(session) or DEFAULT_BOT_USER_NAME


def _remember_bot_user_name(session, name):
    """Record the name so `delete` and `show` need no argument."""
    if session.config.get(BOT_USER_CONFIG_KEY) == name:
        return
    session.config[BOT_USER_CONFIG_KEY] = name
    session.write_config()


def _forget_bot_user_name(session, name):
    if session.config.get(BOT_USER_CONFIG_KEY) != name:
        return
    del session.config[BOT_USER_CONFIG_KEY]
    session.write_config()


def _iam_user_exists(iam, user_name):
    try:
        iam.get_user(UserName=user_name)
        return True
    except ClientError as e:
        if e.response.get('Error', {}).get('Code') == 'NoSuchEntity':
            return False
        raise


def _delete_access_keys(iam, name):
    for page in iam.get_paginator('list_access_keys').paginate(UserName=name):
        for meta in page.get('AccessKeyMetadata', []):
            iam.delete_access_key(UserName=name, AccessKeyId=meta['AccessKeyId'])


def _write_bot_credentials(name, key):
    credentials_path = paths.bot_credentials_path()
    paths.ensure(credentials_path.parent)
    with open(credentials_path, 'w', encoding='utf-8') as handle:
        json.dump({'user_name': name,
                   'access_key_id': key['AccessKeyId'],
                   'secret_access_key': key['SecretAccessKey']}, handle, indent=2)
    try:
        credentials_path.chmod(0o600)
    except OSError:
        pass
    return credentials_path


@bot_user.command('show')
@click.argument('name', required=False)
@pass_session
def show_bot_user(session, name):
    """Report the bot user's name, its IAM state and where its keys are."""
    settings = session.require_aws()
    name = _bot_user_name(session, name)
    credentials_path = paths.bot_credentials_path()
    iam = boto3.client('iam', region_name=settings.region)

    tracked = _tracked_bot_user_name(session)
    record = {'name': name, 'exists': _iam_user_exists(iam, name),
              'credentials': str(credentials_path),
              'credentials_present': credentials_path.is_file()}
    if tracked and tracked != name:
        # The keys on disk belong to somebody else, so they are not this user's.
        record['credentials_belong_to'] = tracked
    if record['exists']:
        policies = iam.list_attached_user_policies(UserName=name).get('AttachedPolicies', [])
        record['policies'] = [p['PolicyArn'] for p in policies] or ['(none)']
        record['access_keys'] = sum(
            len(page.get('AccessKeyMetadata', []))
            for page in iam.get_paginator('list_access_keys').paginate(UserName=name))
    output.emit_kv(None, record)


@bot_user.command('create')
@click.argument('name', required=False)
@click.option('--rotate', is_flag=True,
              help='Replace the keys of an existing user instead of failing.')
@pass_session
def create_bot_user(session, name, rotate):
    """Create the IAM bot user with AdministratorAccess and write its access keys.

    NAME defaults to the bot this checkout already tracks, then to "bot". The name used is saved
    back to test_config.json, so `bot-user delete` needs no argument.
    """
    settings = session.require_aws()
    name = _bot_user_name(session, name)
    iam = boto3.client('iam', region_name=settings.region)
    tracked = _tracked_bot_user_name(session)

    # A second name would overwrite the one credentials file and strand the first user in IAM with
    # a live AdministratorAccess key. A tracked name IAM no longer has is stale, so it does not bite.
    if tracked and tracked != name and _iam_user_exists(iam, tracked):
        output.fail(f"This checkout already tracks IAM user {tracked!r}, and there is one "
                    f"credentials file. Run `morpheus admin bot-user delete` before creating "
                    f"{name!r}, or `morpheus admin bot-user create {tracked} --rotate` to issue it "
                    'a new key.')

    exists = _iam_user_exists(iam, name)
    if exists and not rotate:
        output.fail(f"IAM user {name!r} already exists. Run `morpheus admin bot-user create "
                    '--rotate` to issue a new key, or `morpheus admin bot-user delete` to remove '
                    'the user.')
    if exists:
        # A rotate is the recovery path for a lost credentials file: IAM allows two keys per user,
        # so clear the old ones before asking for another.
        _delete_access_keys(iam, name)
    else:
        iam.create_user(UserName=name, Tags=[{'Key': 'Created-For', 'Value': 'Jenkins+CI'},
                                             {'Key': 'Created-By', 'Value': 'Repository'}])
    iam.attach_user_policy(UserName=name, PolicyArn=ADMINISTRATOR_ACCESS_POLICY_ARN)
    key = iam.create_access_key(UserName=name)['AccessKey']

    credentials_path = _write_bot_credentials(name, key)
    _remember_bot_user_name(session, name)
    output.ok(f"{'Rotated the key of' if exists else 'Created'} IAM user {name} with "
              'AdministratorAccess')
    output.emit_kv(None, {'credentials': str(credentials_path)})


@bot_user.command('delete')
@click.argument('name', required=False)
@pass_session
def delete_bot_user(session, name):
    """Delete the IAM bot user, its access keys and the credentials file."""
    settings = session.require_aws()
    name = _bot_user_name(session, name)
    credentials_path = paths.bot_credentials_path()
    iam = boto3.client('iam', region_name=settings.region)

    # The file is another bot's only when it names a different user. One naming nobody is unusable,
    # so this command is the last chance to clear it.
    owner = _credentials_owner()
    owns_credentials = owner is None or owner == name

    if not _iam_user_exists(iam, name):
        output.info(f"IAM user {name!r} does not exist; nothing to destroy.")
        if owns_credentials and credentials_path.is_file():
            credentials_path.unlink()
            output.ok(f"Removed {credentials_path}")
        _forget_bot_user_name(session, name)
        return

    _delete_access_keys(iam, name)

    try:
        iam.detach_user_policy(UserName=name, PolicyArn=ADMINISTRATOR_ACCESS_POLICY_ARN)
    except ClientError as e:
        if e.response.get('Error', {}).get('Code') != 'NoSuchEntity':
            raise

    iam.delete_user(UserName=name)
    _forget_bot_user_name(session, name)
    if owns_credentials and credentials_path.is_file():
        credentials_path.unlink()
        output.ok(f"Deleted IAM user {name} and removed {credentials_path}")
        return
    output.ok(f"Deleted IAM user {name}")
