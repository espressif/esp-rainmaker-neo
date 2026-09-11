# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus user <id> sharing ...` and `push ...` — incoming shares and push registration."""

import click

from . import output
from .context import pass_user


@click.group('sharing')
def sharing():
    """Accept or reject shares sent to this user."""


@sharing.command('list')
@pass_user
def list_requests(user):
    """List pending sharing requests."""
    requests = user.get_sharing_requests()
    if requests is None:
        output.fail('Failed to retrieve sharing requests')
    if not requests:
        output.info('No pending sharing requests.')
        return
    output.emit_table(['request_id'], [[r] for r in requests], title='Sharing requests')


@sharing.command('accept')
@click.argument('request_id')
@pass_user
def accept(user, request_id):
    """Accept sharing request REQUEST_ID."""
    if not user.accept_sharing_request(request_id):
        output.fail(f"Failed to accept sharing request {request_id}")
    output.ok(f"Accepted sharing request {request_id}")


@sharing.command('reject')
@click.argument('request_id')
@pass_user
def reject(user, request_id):
    """Reject sharing request REQUEST_ID."""
    if not user.reject_sharing_request(request_id):
        output.fail(f"Failed to reject sharing request {request_id}")
    output.ok(f"Rejected sharing request {request_id}")


# --- push clients -----------------------------------------------------------

@click.group('push')
def push():
    """Register this user's mobile clients for push notifications."""


def _registered(endpoint_id):
    if not endpoint_id:
        output.fail('Failed to register the client')
    output.ok('Client registered')
    output.emit_kv(None, {'endpoint_id': endpoint_id})


@push.command('register-ios')
@click.argument('bundle_id')
@click.argument('device_token')
@click.option('--sandbox', is_flag=True, help='Register against APNS_SANDBOX instead of APNS.')
@pass_user
def register_ios(user, bundle_id, device_token, sandbox):
    """Register an iOS client under BUNDLE_ID."""
    _registered(user.register_client(platform_type='APNS_SANDBOX' if sandbox else 'APNS',
                                     mobile_device_token=device_token,
                                     platform_app_name=bundle_id))


@push.command('register-android')
@click.argument('project_id')
@click.argument('device_token')
@pass_user
def register_android(user, project_id, device_token):
    """Register an Android client under PROJECT_ID."""
    _registered(user.register_client(platform_type='GCM', mobile_device_token=device_token,
                                     platform_app_name=project_id))


@push.command('register')
@click.argument('platform')
@click.argument('device_token')
@pass_user
def register(user, platform, device_token):
    """Register a client on PLATFORM, for a platform type the named commands do not cover."""
    _registered(user.register_client(platform, device_token))
