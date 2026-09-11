# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""User-context commands: authentication, MQTT, and the raw API escape hatch."""

import click

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


COMMANDS = [auth, connect, subscribe, publish, read_shadow, api, upload_file]
