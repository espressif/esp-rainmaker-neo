# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus admin platforms ...` — the APNS and FCM platform applications."""

import json

import click

from .. import output
from ..context import pass_user


@click.group('platforms')
def platforms():
    """Register the APNS and FCM platform applications."""


def _read_text(path, what):
    try:
        with open(path, 'r') as handle:
            return handle.read()
    except OSError as e:
        output.fail(f"Could not read the {what}: {e}")


_IOS_ARGS = [
    click.argument('p8_key_file', type=click.Path(exists=True, dir_okay=False)),
    click.argument('key_id'),
    click.argument('team_id'),
    click.argument('bundle_id'),
    click.option('--sandbox', is_flag=True, help='Register against the APNS sandbox.'),
]


def _ios_options(f):
    for decorator in reversed(_IOS_ARGS):
        f = decorator(f)
    return f


@platforms.command('register-ios')
@_ios_options
@pass_user
def register_ios(user, p8_key_file, key_id, team_id, bundle_id, sandbox):
    """Register the APNS platform application."""
    if not user.register_ios_platform(_read_text(p8_key_file, 'P8 key'), key_id, team_id,
                                      bundle_id, sandbox):
        output.fail('Failed to register the iOS platform')
    output.ok(f"Registered the iOS platform ({'sandbox' if sandbox else 'production'})")


@platforms.command('update-ios')
@_ios_options
@pass_user
def update_ios(user, p8_key_file, key_id, team_id, bundle_id, sandbox):
    """Update the APNS platform application."""
    if not user.update_mobile_platform(platform='APNS',
                                       authentication_key=_read_text(p8_key_file, 'P8 key'),
                                       key_id=key_id, team_id=team_id, bundle_id=bundle_id,
                                       apns_sandbox=sandbox):
        output.fail('Failed to update the iOS platform')
    output.ok(f"Updated the iOS platform ({'sandbox' if sandbox else 'production'})")


def _android_key(path):
    content = _read_text(path, 'service account JSON')
    try:
        json.loads(content)
    except json.JSONDecodeError as e:
        output.fail(f"Invalid JSON in {path}: {e}")
    return content


@platforms.command('register-android')
@click.argument('service_account_json', type=click.Path(exists=True, dir_okay=False))
@pass_user
def register_android(user, service_account_json):
    """Register the FCM platform application."""
    if not user.register_android_platform(_android_key(service_account_json)):
        output.fail('Failed to register the Android platform')
    output.ok('Registered the Android platform')


@platforms.command('update-android')
@click.argument('service_account_json', type=click.Path(exists=True, dir_okay=False))
@pass_user
def update_android(user, service_account_json):
    """Update the FCM platform application."""
    if not user.update_mobile_platform(platform='GCM',
                                       api_key=_android_key(service_account_json)):
        output.fail('Failed to update the Android platform')
    output.ok('Updated the Android platform')


@platforms.command('list')
@pass_user
def list_platforms(user):
    """List the registered platform applications."""
    # The old CLI called this and never looked at the result, so a failure read as a success.
    result = user.list_mobile_platforms()
    if result is None:
        output.fail('Failed to list the mobile platforms')
    output.emit_table(['integration_id', 'integration_type'],
                      [[entry.get('integration_id'), entry.get('integration_type')]
                       for entry in result.get('integrations', [])])


@platforms.command('delete')
@click.argument('platform_name')
@click.argument('platform_app_name')
@pass_user
def delete_platform(user, platform_name, platform_app_name):
    """Delete platform application PLATFORM_APP_NAME on PLATFORM_NAME."""
    if not user.delete_mobile_platform(platform_name, platform_app_name):
        output.fail('Failed to delete the mobile platform')
    output.ok(f"Deleted mobile platform {platform_app_name}")
