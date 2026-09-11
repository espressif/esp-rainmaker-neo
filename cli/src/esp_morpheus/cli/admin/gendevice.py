# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""`morpheus admin test-data gen-device` — a certificate and key for a node, generated locally.

It reaches nothing and needs no credentials. It sits with `test-data` because it owns the same
test_config.json, and the node it writes there does nothing until `test-data setup` registers it.
"""

import click

from ...sdk.device import generate_key_and_cert
from .. import output
from ..context import pass_session


@click.command('gen-device')
@click.argument('node_name')
@click.argument('key_type', type=click.Choice(['rsa', 'ec']))
@click.option('--stdout', 'to_stdout', is_flag=True,
              help='Print the certificate and key instead of writing test_config.json.')
@click.option('--force', is_flag=True,
              help='Replace the certificate when NODE_NAME is already in test_config.json.')
@pass_session
def gen_device(session, node_name, key_type, to_stdout, force):
    """Generate a self-signed certificate and key for NODE_NAME.

    Needs no deployment and no credentials. The node is appended to test_config.json, which is
    written from the packaged defaults first when it does not exist yet. Register it with
    `morpheus admin test-data setup`, which re-runs idempotently over the nodes already seeded.
    """
    key_pem, cert_pem = generate_key_and_cert(node_name, key_type)
    entry = {'thing_name': node_name, 'cert': cert_pem, 'key': key_pem}

    if to_stdout:
        output.emit_kv(None, entry)
        return

    if not session.config_exists():
        session.generate_config()

    nodes = session.config.setdefault('nodes', [])
    index = next((i for i, n in enumerate(nodes) if n.get('thing_name') == node_name), None)
    if index is None:
        nodes.append(entry)
        index = len(nodes) - 1
        action = 'Added'
    elif force:
        # Updated, not replaced, so node_cfg and associate_to survive a certificate regeneration.
        nodes[index].update(entry)
        action = 'Regenerated the certificate of'
    else:
        output.fail(f"{node_name} is already node {index} in test_config.json. Pass --force to "
                    'regenerate its certificate, which breaks the deployed node until the next '
                    '`admin test-data setup`.')

    path = session.write_config()
    output.ok(f"{action} {node_name} as node {index}")
    output.emit_kv(None, {'thing_name': node_name, 'key_type': key_type,
                          'index': index, 'config': str(path)})
