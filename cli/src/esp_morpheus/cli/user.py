# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""User-context commands: authentication, MQTT, and the raw API escape hatch."""

import click

from ..sdk.group import Group
from . import output
from .context import pass_user, pass_unverified_user


@click.command()
@pass_unverified_user
def auth(user):
    """Authenticate, provisioning the account if it does not exist yet."""
    output.info(f"Authenticating {user.username}")
    authenticated = user.get_aws_credentials()

    # Provisioning runs either way, because the first sign-in for an account that does not exist
    # yet necessarily fails — that failure is the path that creates it, not an error.
    # Admins go into the admin pool; end users into the provider pool they sign in against.
    if user.is_admin:
        user.create_admin_via_cognito()
    else:
        user.register_user_via_lambda(email=user.username if '@' in user.username else None)

    if not authenticated and not user.get_aws_credentials():
        raise output.AuthError(
            f"Authentication failed for {user.username}. The account was provisioned; if it is new "
            "it may need its email verified before the first sign-in.")
    output.ok(f"Authenticated {user.username}")


@click.command()
@pass_user
def connect(user):
    """Assume the user's IoT role and open an MQTT connection."""
    credentials = user.assume_role()
    if not credentials:
        raise output.AuthError('Failed to assume the user role')
    output.debug('Role assumed')
    if not user.mqtt_connect(credentials):
        output.fail('Failed to connect to MQTT')
    output.ok('Connected to MQTT')


@click.command()
@click.argument('thing_name')
@click.argument('shadows', nargs=-1, required=True)
@pass_user
def subscribe(user, thing_name, shadows):
    """Subscribe to one or more named shadows of THING_NAME."""
    if not user.subscribe_to_named_shadows(thing_name, list(shadows)):
        output.fail(f"Failed to subscribe to named shadows for '{thing_name}'")
    output.ok(f"Subscribed to {', '.join(shadows)} for '{thing_name}'")


@click.command()
@click.argument('thing_name')
@click.argument('topic')
@click.argument('data', nargs=-1, required=True)
@click.option('--file', 'file_path', type=click.Path(exists=True, dir_okay=False),
              help='Read the JSON payload from a file instead of the command line.')
@pass_user
def publish(user, thing_name, topic, data, file_path):
    """Publish a JSON payload to TOPIC for THING_NAME."""
    payload = output.parse_json_arg(' '.join(data), file_path)
    if not user.mqtt_publish_to_topic(thing_name, topic, payload):
        output.fail(f"Failed to publish to '{topic}' for '{thing_name}'")
    output.ok(f"Published to '{topic}' for '{thing_name}'")


@click.command('read-shadow')
@click.argument('thing_name')
@click.argument('shadow_name')
@pass_user
def read_shadow(user, thing_name, shadow_name):
    """Request a read of SHADOW_NAME on THING_NAME."""
    if not user.read_shadow(thing_name, shadow_name):
        output.fail(f"Failed to request shadow '{shadow_name}' on '{thing_name}'")
    output.ok(f"Requested shadow '{shadow_name}' on '{thing_name}'")


def params_shadow_name_for(user, node_id):
    """Name of the params shadow a node currently reports into, or None if the node is not in any
    of this user's groups. The group is part of the shadow name, so it has to be resolved before
    the shadow can be read."""
    for grp in Group(user).list_groups().get('groups', []):
        if node_id in (grp.get('node_ids') or []):
            return f"params-{grp['group_id']}"
    return None


@click.command('get-shadow')
@click.argument('node_id')
@click.argument('shadow_name', required=False)
@pass_user
def get_shadow(user, node_id, shadow_name):
    """Print NODE_ID's named shadow once.

    SHADOW_NAME defaults to the params shadow of the group the node is in. `subscribe` streams
    every later update and never terminates, which is awkward when the question is just what the
    shadow says right now.
    """
    if not shadow_name:
        shadow_name = params_shadow_name_for(user, node_id)
        if not shadow_name:
            output.fail(f"Node {node_id} is in none of this user's groups; pass SHADOW_NAME")

    shadow = user.get_named_shadow(node_id, shadow_name)
    if shadow is None:
        output.fail(f"No shadow '{shadow_name}' on node {node_id}")
    output.emit_json(shadow)


# --- raw API ----------------------------------------------------------------

@click.group()
def api():
    """Call the RainMaker API directly. PATH is relative to the API root."""


def _body(data, file_path):
    payload = output.parse_json_arg(' '.join(data), file_path)
    return payload


@api.command('get')
@click.argument('path')
@pass_user
def api_get(user, path):
    """GET /PATH."""
    output.emit_response(user.make_api_request('GET', f'/{path.lstrip("/")}'))


@api.command('post')
@click.argument('path')
@click.argument('data', nargs=-1)
@click.option('--file', 'file_path', type=click.Path(exists=True, dir_okay=False))
@pass_user
def api_post(user, path, data, file_path):
    """POST DATA to /PATH."""
    output.emit_response(user.make_api_request('POST', f'/{path.lstrip("/")}',
                                               data=_body(data, file_path)))


@api.command('put')
@click.argument('path')
@click.argument('data', nargs=-1)
@click.option('--file', 'file_path', type=click.Path(exists=True, dir_okay=False))
@pass_user
def api_put(user, path, data, file_path):
    """PUT DATA to /PATH."""
    output.emit_response(user.make_api_request('PUT', f'/{path.lstrip("/")}',
                                               data=_body(data, file_path)))


@api.command('patch')
@click.argument('path')
@click.argument('data', nargs=-1)
@click.option('--file', 'file_path', type=click.Path(exists=True, dir_okay=False))
@pass_user
def api_patch(user, path, data, file_path):
    """PATCH /PATH with DATA."""
    # skip_cors_check: PATCH OPTIONS routes are commonly absent in API Gateway, and the preflight
    # is a deploy-config check rather than part of the operation.
    output.emit_response(user.make_api_request('PATCH', f'/{path.lstrip("/")}',
                                               data=_body(data, file_path), skip_cors_check=True))


@api.command('delete')
@click.argument('path')
@pass_user
def api_delete(user, path):
    """DELETE /PATH."""
    output.emit_response(user.make_api_request('DELETE', f'/{path.lstrip("/")}',
                                               skip_cors_check=True))


@click.command('upload-file')
@click.argument('file_type')
@click.argument('file_path', type=click.Path(exists=True, dir_okay=False))
@pass_user
def upload_file(user, file_type, file_path):
    """Upload FILE_PATH through the file API as FILE_TYPE (e.g. node_cert)."""
    success, result = user.upload_file(file_path, file_type)
    if not success:
        output.fail(f"Upload failed: {result}")
    output.emit_kv('Uploaded', {'local file': file_path, 'S3 location': result})


COMMANDS = [auth, connect, subscribe, publish, read_shadow, get_shadow, api, upload_file]
