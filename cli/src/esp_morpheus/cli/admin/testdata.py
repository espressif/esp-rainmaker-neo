# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus admin test-data ...` — seeding a deployment with the fixtures the suite expects.

Not needed to drive an account that already exists: `morpheus user <email>` reaches any provisioned
identity. Seeding provisions users in Cognito and things in IoT, so it needs AWS credentials.
"""

import hashlib
import threading
from concurrent.futures import ThreadPoolExecutor

import boto3
import click
from botocore.config import Config as BotocoreConfig
from botocore.exceptions import ClientError
from cryptography import x509
from cryptography.hazmat.primitives import serialization

from ...sdk.group import Group
from .. import output
from ..context import pass_session

# Thing-name prefix the integration tests generate ("test-<key_type>-device-<uuid>" and similar).
TEST_THING_PREFIX = 'test-'

# IoT's control-plane list and delete APIs throttle at a low rate. Adaptive retry adds client-side
# rate limiting, and a modest pool keeps the sweep inside the service limit; more concurrency just
# converts into ThrottlingExceptions.
CERT_SWEEP_WORKERS = 8
_SWEEP_RETRY_CONFIG = BotocoreConfig(retries={'max_attempts': 10, 'mode': 'adaptive'})

_sweep_local = threading.local()


@click.group('test-data')
def test_data():
    """Seed and remove the test users, nodes and groups."""


@test_data.command('setup')
@pass_session
def setup(session):
    """Provision every user and node in test_config.json, writing the file if it is absent."""
    session.require_aws()
    if not session.config_exists():
        session.generate_config()

    user_map = _setup_users(session)
    _setup_nodes(session, user_map)
    output.ok('Setup complete')


@test_data.command('destroy')
@click.option('--dry-run', is_flag=True, help='Report what the sweep would delete, and delete nothing.')
@pass_session
def destroy(session, dry_run):
    """Remove the test nodes, their groups and any leftover test certificates.

    User accounts are left alone; there is no destroy operation for them.
    """
    session.require_aws()
    for index in range(len(session.config.get('nodes', []))):
        try:
            device = session.get_node(str(index))
        except click.ClickException as e:
            output.err(e.format_message())
            continue
        try:
            if device.destroy_test_node():
                output.ok(f"Destroyed node {device.node_thing_name}")
            else:
                output.err(f"Failed to destroy node {device.node_thing_name}")
        except Exception as e:  # noqa: BLE001 - one bad node must not stop the sweep
            output.err(f"Error destroying {device.node_thing_name}: {e}")

    for index in range(len(session.config.get('users', []))):
        try:
            _delete_user_groups(session.get_user(str(index)))
        except Exception as e:  # noqa: BLE001
            output.err(f"Failed to delete groups for user {index}: {e}")

    sweep_test_certs(session, dry_run)
    output.info('User accounts are not destroyed by this command.')


def _delete_user_groups(user):
    api = Group(user)
    for group in api.list_groups().get('groups', []):
        try:
            api.delete_group(group['group_id'])
            output.debug(f"Deleted group {group['group_id']}")
        except Exception as e:  # noqa: BLE001
            output.err(f"Failed to delete group {group.get('group_id', 'unknown')}: {e}")


def _setup_users(session):
    user_map = {}
    output.info('Setting up users...')
    for index in range(len(session.config.get('users', []))):
        user = session.get_user(str(index))
        # Admins go into the admin pool; end users into the provider pool they sign in against.
        if user.is_admin:
            user.create_admin_via_cognito()
            output.ok(f"Provisioned admin {user.username}")
        else:
            user.register_user_via_lambda(email=user.username if '@' in user.username else None)
            output.ok(f"Provisioned user {user.username}")
        user_map[user.username] = user

        api = Group(user)
        groups = api.list_groups().get('groups') or []
        if not groups:
            group_id = api.create_group('Home')
            if group_id:
                user.add_group_id(group_id)
                output.ok(f"Created the default Home group for {user.username} ({group_id})")
            else:
                output.err(f"Failed to create the default Home group for {user.username}")
            continue

        output.debug(f"{user.username} already has {len(groups)} group(s); skipping creation")
        for position, group in enumerate(groups):
            user.add_group_id(group['group_id'])
            if position == 0:
                for node in group.get('node_ids', []):
                    user.add_device(node)
    return user_map


def _setup_nodes(session, user_map):
    nodes = session.config.get('nodes', [])
    admin_group_name = session.config.get('admin_group_name')

    # Register through the admin API rather than invoking the registration Lambda: API Gateway
    # supplies the caller identity the handler authorizes against. Re-registering an existing cert
    # is idempotent server-side, which keeps repeated runs safe.
    admin = next((u for u in user_map.values() if u.is_admin), None)
    if not admin:
        output.fail('No admin in test_config.json; node registration would be unauthorized.')

    output.info('Setting up devices...')
    for index, node_config in enumerate(nodes):
        device = session.get_node(str(index))
        if admin.register_node(device, tags=['created_by:test'],
                               admin_group_names=[admin_group_name]):
            output.ok(f"Registered node {device.node_thing_name}")
        else:
            # Most often the node is already registered, from an earlier run or another checkout
            # that seeded this deployment under the same fixed thing name. The rest of the setup
            # still applies, so carry on rather than leaving the node half-seeded.
            output.warn(f"Node {device.node_thing_name} was not registered (already registered, or "
                        "the call failed); continuing with the certificate and association")

        if not _ensure_cert_registered(session, device):
            output.err(f"Skipping association for {device.node_thing_name}: its certificate is not "
                       'usable against this deployment')
            continue

        associate_to = node_config.get('associate_to')
        if not associate_to:
            continue
        user = user_map.get(associate_to)
        if not user:
            output.warn(f"User {associate_to} not found; skipping association")
            continue
        if device.node_thing_name in user.get_devices():
            output.debug(f"{associate_to} already has {device.node_thing_name}")
            continue
        group_id = user.get_group_ids()[0]
        error = user.do_user_node_assoc(device, group_id)
        if error:
            output.err(f"Failed to associate {device.node_thing_name} with {associate_to}: {error}")
        else:
            output.ok(f"Associated {device.node_thing_name} with {associate_to} in {group_id}")


def _cert_id_from_pem(cert_pem):
    """AWS IoT's certificate ID: the lowercase hex SHA-256 of the certificate's DER bytes.

    Computed locally so a cert can be looked up without first knowing its ARN, mirroring
    iotutil.GetCertIDFromPEM on the backend.
    """
    der = x509.load_pem_x509_certificate(cert_pem.encode()).public_bytes(serialization.Encoding.DER)
    return hashlib.sha256(der).hexdigest()


def _ensure_cert_registered(session, device):
    """Attach this checkout's certificate to the device's thing in the target deployment.

    Thing names are fixed in test_config.json but its certificates are generated per checkout, so a
    deployment somebody else seeded holds those thing names against *their* certificate. Registering
    ours directly is idempotent — a thing may carry several principals — and without it the device
    fails its TLS handshake at connect time.
    """
    settings = session.settings
    client = boto3.client('iot', region_name=settings.region)
    thing_name = device.node_thing_name

    try:
        cert_id = _cert_id_from_pem(device.node_cert)
    except Exception as e:  # noqa: BLE001
        output.err(f"Could not read the certificate for {thing_name}: {e}")
        return False

    try:
        try:
            cert_arn = client.describe_certificate(
                certificateId=cert_id)['certificateDescription']['certificateArn']
        except client.exceptions.ResourceNotFoundException:
            cert_arn = client.register_certificate_without_ca(
                certificatePem=device.node_cert, status='ACTIVE')['certificateArn']
            output.debug(f"Registered this checkout's certificate for {thing_name} ({cert_id[:12]})")

        # A cert left INACTIVE by an earlier destroy still exists, and the broker then closes the
        # connection without a usable error, so normalise the status every time.
        client.update_certificate(certificateId=cert_id, newStatus='ACTIVE')

        attached = [p['policyName'] for p in
                    client.list_attached_policies(target=cert_arn)['policies']]
        if settings.default_thing_policy not in attached:
            client.attach_policy(policyName=settings.default_thing_policy, target=cert_arn)

        try:
            client.describe_thing(thingName=thing_name)
        except client.exceptions.ResourceNotFoundException:
            client.create_thing(thingName=thing_name)

        if cert_arn not in client.list_thing_principals(thingName=thing_name)['principals']:
            client.attach_thing_principal(thingName=thing_name, principal=cert_arn)
            output.debug(f"Attached the local certificate to thing {thing_name}")
    except ClientError as e:
        output.err(f"Failed to attach the local certificate to {thing_name}: {e}")
        return False
    return True


# --- certificate sweep ------------------------------------------------------

def _is_test_thing(session, thing_name):
    """Whether a thing came from this repo's test tooling.

    Test nodes share DefaultThingPolicy with every other node in the deployment, so the name is the
    only thing that identifies them. Anything outside the prefix and the local config is left alone.
    """
    if thing_name.startswith(TEST_THING_PREFIX):
        return True
    return any(n.get('thing_name') == thing_name for n in session.config.get('nodes', []))


def _sweep_iot_client(region):
    """One IoT client per worker thread, so the sweep is not serialised on a shared pool."""
    if not hasattr(_sweep_local, 'iot'):
        _sweep_local.iot = boto3.client('iot', region_name=region, config=_SWEEP_RETRY_CONFIG)
    return _sweep_local.iot


def _find_test_things(session):
    """Names of things this repo's test tooling created, the cheapest way the account allows.

    Fleet indexing answers the prefix directly, one query instead of a walk. Without it, enumerate
    the registry and filter client-side.
    """
    region = session.settings.region
    client = boto3.client('iot', region_name=region, config=_SWEEP_RETRY_CONFIG)
    names, token = set(), None
    try:
        while True:
            kwargs = {'queryString': f'thingName:{TEST_THING_PREFIX}*', 'maxResults': 250}
            if token:
                kwargs['nextToken'] = token
            response = client.search_index(**kwargs)
            names.update(t['thingName'] for t in response.get('things', []))
            token = response.get('nextToken')
            if not token:
                break
    except (client.exceptions.IndexNotReadyException,
            client.exceptions.InvalidRequestException) as e:
        output.warn(f"Fleet indexing unavailable ({type(e).__name__}); listing the registry instead.")
        names.clear()
        for page in client.get_paginator('list_things').paginate():
            names.update(t['thingName'] for t in page.get('things', [])
                         if t['thingName'].startswith(TEST_THING_PREFIX))

    # test_config.json names do not carry the prefix, so add them explicitly.
    names.update(n['thing_name'] for n in session.config.get('nodes', []) if n.get('thing_name'))
    return sorted(names)


def _sweep_one_thing(session, thing_name, dry_run=False):
    """Delete one test thing and any certificate left holding nothing else.

    A certificate shared with an out-of-scope thing is left intact, since detaching it would break a
    node this tool does not own. Failures are counted, so a throttled sweep cannot look complete.
    """
    client = _sweep_iot_client(session.settings.region)
    try:
        certs = []
        for page in client.get_paginator('list_thing_principals').paginate(thingName=thing_name):
            certs.extend(page.get('principals', []))

        deletable, skipped = [], 0
        for cert_arn in certs:
            siblings = []
            for page in client.get_paginator('list_principal_things').paginate(principal=cert_arn):
                siblings.extend(page.get('things', []))
            if all(_is_test_thing(session, t) for t in siblings):
                deletable.append(cert_arn)
            else:
                skipped += 1

        if dry_run:
            return 1, len(deletable), skipped, 0

        for cert_arn in certs:
            client.detach_thing_principal(thingName=thing_name, principal=cert_arn)
        client.delete_thing(thingName=thing_name)

        deleted_certs = 0
        for cert_arn in deletable:
            try:
                # Whatever policies the cert carries must come off before it can be deleted; do not
                # assume it is only on DefaultThingPolicy.
                for page in client.get_paginator('list_attached_policies').paginate(target=cert_arn):
                    for policy in page.get('policies', []):
                        client.detach_policy(policyName=policy['policyName'], target=cert_arn)
                certificate_id = cert_arn.split('/')[-1]
                client.update_certificate(certificateId=certificate_id, newStatus='INACTIVE')
                client.delete_certificate(certificateId=certificate_id)
                deleted_certs += 1
            except Exception as e:  # noqa: BLE001
                output.err(f"Failed to delete certificate {cert_arn}: {e}")
        return 1, deleted_certs, skipped, 0
    except client.exceptions.ResourceNotFoundException:
        return 0, 0, 0, 0
    except Exception as e:  # noqa: BLE001
        output.err(f"Failed to process thing {thing_name}: {e}")
        return 0, 0, 0, 1


def sweep_test_certs(session, dry_run=False):
    """Delete leftover test things and their certificates.

    Driven from the things rather than from DefaultThingPolicy's targets: that policy is attached to
    every node in the deployment, so scanning it costs one API call per node in the account. Cost
    now scales with what the tests left behind, not with the size of the deployment.
    """
    try:
        things = _find_test_things(session)
    except Exception as e:  # noqa: BLE001
        output.err(f"Failed to find test things: {e}")
        return

    if not things:
        output.info('Cert sweep: no test things found.')
        return

    output.info(f"Sweeping {len(things)} candidate test thing(s)"
                f"{' (dry run)' if dry_run else ''}...")
    deleted_things = deleted_certs = skipped_certs = failures = 0
    with ThreadPoolExecutor(max_workers=CERT_SWEEP_WORKERS) as pool:
        for t, c, s, f in pool.map(lambda n: _sweep_one_thing(session, n, dry_run), things):
            deleted_things += t
            deleted_certs += c
            skipped_certs += s
            failures += f

    verb = 'would delete' if dry_run else 'deleted'
    output.ok(f"Cert sweep: {verb} {deleted_things} thing(s) and {deleted_certs} certificate(s); "
              f"left {skipped_certs} certificate(s) shared with non-test things untouched.")
    if failures:
        output.warn(f"{failures} thing(s) could not be processed — the sweep is incomplete. "
                    'Re-run `morpheus admin test-data destroy` to retry them.')
