# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`esp-morpheus user <id> sharing ...` and `push ...` — pending shares and push registration."""

import time
from datetime import datetime

import click

from ..sdk.group import Group
from . import output
from .context import pass_user


@click.group('sharing')
def sharing():
    """List, accept or reject pending shares."""


def _primary(request):
    """Who sent the request: their email, else their phone number, else their user ID."""
    return output.cell(request.get('primary_email') or request.get('primary_phone_number')
                       or request.get('primary_user_id'))


def _expires(epoch, now=None):
    """An expiry in local time with the time left: an epoch 18h away -> '2026-11-14 09:30 (in 18h)'."""
    if not epoch:
        return '-'
    left = int(epoch - (now if now is not None else time.time()))
    if left <= 0:
        remaining = 'expired'
    elif left < 3600:
        remaining = f"in {max(left // 60, 1)}m"
    elif left < 86400:
        remaining = f"in {left // 3600}h"
    else:
        remaining = f"in {left // 86400}d"
    return f"{datetime.fromtimestamp(epoch):%Y-%m-%d %H:%M} ({remaining})"


def _emit_requests(entries, columns, row, title):
    if output.structured():
        output.emit_json(entries)
        return
    if not entries:
        output.info('No pending sharing requests.')
        return
    output.emit_table(columns, [row(e) for e in entries], title=title)


@sharing.command('received')
@pass_user
def received(user):
    """List the sharing requests sent to this user."""
    response = user.get_sharing_requests_raw()
    if response.status_code != 200:
        output.fail_response('list received sharing requests', response)
    _emit_requests(
        response.json().get('sharing_requests') or [],
        ['request_id', 'group_id', 'subgroup', 'access', 'primary'],
        lambda r: [output.cell(r.get('sharing_request_id')), output.cell(r.get('group_id')),
                   output.cell(r.get('subgroup_id')), output.cell(r.get('access_type')), _primary(r)],
        'Sharing requests received')


@sharing.command('sent')
@click.argument('group_id')
@click.option('--subgroup', 'subgroup_id', help='Only the requests for this subgroup.')
@click.option('--sent-by', help='Only the requests this user sent: a user ID, or "me".')
@pass_user
def sent(user, group_id, subgroup_id, sent_by):
    """List the pending sharing requests GROUP_ID sent, whoever in the group sent them."""
    api = Group(user)
    entries, start_key = [], None
    while True:
        response = api.list_group_sharing_requests_raw(group_id, subgroup_id, sent_by, start_key=start_key)
        if response.status_code != 200:
            output.fail_response(f"list sharing requests sent from group {group_id}", response)
        body = response.json()
        entries.extend(body.get('sharing_requests') or [])
        start_key = body.get('next_key')
        if not start_key:
            break

    _emit_requests(
        entries,
        ['request_id', 'target', 'access', 'subgroup', 'primary', 'expires'],
        lambda r: [output.cell(r.get('sharing_request_id')),
                   '(QR code)' if r.get('is_qr') else output.cell(r.get('target_username')),
                   output.cell(r.get('access_type')), output.cell(r.get('subgroup_id')), _primary(r),
                   _expires(r.get('expiration_time'))],
        f"Sharing requests sent from group {group_id}")


def _process(user, request_id, action, done):
    response = user.process_sharing_request_raw(request_id, action)
    if response.status_code not in (200, 201):
        output.fail_response(f"{action} sharing request {request_id}", response)
    output.ok(f"{done} sharing request {request_id}")


@sharing.command('accept')
@click.argument('request_id')
@pass_user
def accept(user, request_id):
    """Accept sharing request REQUEST_ID."""
    _process(user, request_id, 'accept', 'Accepted')


@sharing.command('reject')
@click.argument('request_id')
@pass_user
def reject(user, request_id):
    """Reject REQUEST_ID. The primary user who sent it can reject it to cancel."""
    _process(user, request_id, 'reject', 'Rejected')


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
