# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus user <id> matter ...` — the three-step Matter commissioning exchange.

Each step used to be a token in the middle of `matter_assoc <group_id> <step> ...`, hand-dispatched
after the group id had already been consumed. They are real subcommands now, so click checks their
arguments and `help matter verify` says what they take.
"""

import click
from cryptography import x509
from cryptography.hazmat.primitives import serialization

from ..sdk.matter import do_confirm, do_initiate, do_verify_with_nocsr_elements
from . import output
from .context import pass_user
from .groups import split_csv

CHIP_TOOL_HINT = 'In interactive chip-tool (chip-tool interactive start --commissioner-name gamma):'


@click.group('matter')
def matter():
    """Commission a Matter node into a fabric."""


def _stored(user):
    return getattr(user, '_matter_assoc_state', {})


def _request_id(user, given):
    request_id = given or _stored(user).get('request_id')
    if not request_id:
        output.fail("No stored request id. Run `matter initiate <group_id>` first, "
                    "or pass --request-id.")
    return request_id


@matter.command('initiate')
@click.argument('group_id')
@pass_user
def initiate(user, group_id):
    """Start commissioning into GROUP_ID and print the attestation challenge."""
    request_id, challenge = do_initiate(user, group_id)
    if request_id is None:
        output.fail(f"Initiate failed: {challenge}")

    user._matter_assoc_state = {
        'request_id': request_id,
        'challenge': challenge,
        'group_id': group_id,
    }
    output.emit_kv('Commissioning started', {'request_id': request_id, 'challenge': challenge})
    output.info(CHIP_TOOL_HINT)
    output.info(f"    operationalcredentials csrrequest hex:{challenge} 2 0")


@matter.command('verify')
@click.argument('group_id')
@click.argument('nocsr_elements_hex')
@click.argument('attestation_challenge_hex')
@click.argument('attestation_signature_hex')
@click.option('--request-id', help='Defaults to the request id from `matter initiate`.')
@pass_user
def verify(user, group_id, nocsr_elements_hex, attestation_challenge_hex,
           attestation_signature_hex, request_id):
    """Submit the node's CSR and attestation for GROUP_ID."""
    request_id = _request_id(user, request_id)
    try:
        blobs = [bytes.fromhex(value) for value in
                 (nocsr_elements_hex, attestation_challenge_hex, attestation_signature_hex)]
    except ValueError as e:
        raise click.BadParameter(f"not valid hex: {e}")

    result, error = do_verify_with_nocsr_elements(user, group_id, request_id, *blobs)
    if error:
        output.fail(f"Verify failed: {error}")

    output.emit_kv('Verified', {'NOC': result.get('noc', 'N/A'),
                                'Matter node ID': result.get('matter_node_id', 'N/A')})
    user._matter_assoc_state = {**_stored(user), 'verify_result': result}
    output.info(CHIP_TOOL_HINT)
    output.info('    operationalcredentials add-trusted-root-certificate hex:<rca as tlv> 2 0')
    output.info('    operationalcredentials add-noc hex:<noc as tlv> hex:<ipk as tlv> '
                '<user node id> 0x131B 2 0')


@matter.command('confirm')
@click.argument('group_id')
@click.option('--request-id', help='Defaults to the request id from `matter initiate`.')
@click.option('--capabilities', callback=split_csv, help='Comma-separated capabilities to enable.')
@pass_user
def confirm(user, group_id, request_id, capabilities):
    """Complete commissioning into GROUP_ID."""
    request_id = _request_id(user, request_id)
    result = do_confirm(user, group_id, request_id, capabilities or None)
    if result is not True:
        output.fail(f"Confirm failed: {result}")
    user._matter_assoc_state = {}
    output.ok('Commissioning complete')
    if capabilities:
        output.info(f"Enabled capabilities: {', '.join(capabilities)}")


@matter.command('get-noc')
@click.argument('group_id')
@pass_user
def get_noc(user, group_id):
    """Print this user's operational certificate for GROUP_ID, in PEM and DER hex."""
    result = user.get_matter_noc(group_id)
    if not result:
        output.fail('Failed to get the Matter NOC')

    noc_pem = result.get('noc', 'N/A')
    key_pem = user.matter_private_key.private_bytes(
        serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8,
        serialization.NoEncryption()).decode()
    output.emit_kv('Matter NOC', {
        'Matter node ID': result.get('matter_node_id', 'N/A'),
        'NOC': noc_pem,
        'NOC (hex)': _cert_hex(noc_pem),
        'Private key': key_pem,
        'Private key (hex)': _key_hex(key_pem),
    })


def _cert_hex(pem):
    if pem == 'N/A':
        return 'N/A'
    return x509.load_pem_x509_certificate(pem.encode()).public_bytes(
        serialization.Encoding.DER).hex()


def _key_hex(pem):
    return serialization.load_pem_private_key(pem.encode(), password=None).private_bytes(
        serialization.Encoding.DER, serialization.PrivateFormat.PKCS8,
        serialization.NoEncryption()).hex()
