# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
prov_ble.py — esp_prov / protocomm glue for app_sim's interactive `prov` command.

Keeps the async, esp_prov-specific BLE plumbing out of app_sim.py.

BLE provisioning is opt-in. The esp_prov dependency is heavy — it vendors esp_prov
from idf-extra-components (a network clone) and needs a full ESP-IDF checkout
(IDF_PATH) for the protocomm python protobuf files — so it is NOT pulled in when
this module is imported. Call ensure_esp_prov() once before using any helper below;
app_sim does this lazily when the operator actually runs `prov`, so plain app-sim
sessions never touch IDF or the network.
"""

import json
import os
import pathlib
import sys

from cryptography import x509
from cryptography.hazmat.primitives.asymmetric import ec

from .. import paths
from .sparse_dep import SparseDependency

# esp_prov + protobuf bindings, populated by ensure_esp_prov(). Kept at module
# scope so the async helpers below can reference them once bootstrapped.
esp_prov = None
str_to_bytes = None
ch_resp_pb = None
claim_pb = None

_bootstrapped = False


def ensure_esp_prov():
    """Vendor esp_prov and import it, once (idempotent).

    Deferred out of module import because it is network- and IDF-dependent: it
    fetches esp_prov from idf-extra-components and requires IDF_PATH for the IDF
    protocomm python files. Only invoked when provisioning is actually requested.

    Set ESP_PROV_PATH to an existing esp_prov tool directory to skip the clone
    entirely (e.g. point it at network_provisioning/tool/esp_prov in your own
    idf-extra-components checkout).

    Raises RuntimeError with an actionable message when IDF_PATH is missing.
    """
    global esp_prov, str_to_bytes, ch_resp_pb, claim_pb, _bootstrapped
    if _bootstrapped:
        return

    # esp_prov requires IDF_PATH to locate the protocomm python protobuf files.
    # Check first, so we fail with a clear message before any network fetch.
    if "IDF_PATH" not in os.environ:
        raise RuntimeError(
            "BLE provisioning requires IDF_PATH (esp_prov needs the ESP-IDF "
            "protocomm python files). Re-run with IDF_PATH pointing at your "
            "ESP-IDF checkout, e.g.:\n"
            "  IDF_PATH=~/esp/esp-idf esp-morpheus app-sim <user>"
        )

    # Locate the esp_prov tool dir: honour a pre-existing checkout via ESP_PROV_PATH,
    # otherwise vendor network_provisioning from idf-extra-components on demand.
    esp_prov_override = os.environ.get("ESP_PROV_PATH")
    if esp_prov_override:
        esp_prov_path = pathlib.Path(esp_prov_override).expanduser().resolve()
        if not esp_prov_path.exists():
            raise RuntimeError(
                f"ESP_PROV_PATH does not exist: {esp_prov_path}"
            )
    else:
        network_prov_dir = SparseDependency(
            repo="https://github.com/espressif/idf-extra-components.git",
            subdir="network_provisioning",
            ref="master",
            dest=paths.vendor_dir() / "idf-extra-components",
        ).ensure()
        esp_prov_path = (network_prov_dir / "tool" / "esp_prov").resolve()
    if str(esp_prov_path) not in sys.path:
        sys.path.insert(0, str(esp_prov_path))

    # Make the generated challenge-response protobuf importable.
    chal_resp_proto_path = (pathlib.Path(__file__).parent / "chal_resp_proto").resolve()
    if str(chal_resp_proto_path) not in sys.path:
        sys.path.insert(0, str(chal_resp_proto_path))

    import esp_prov as _esp_prov
    import esp_rmaker_chal_resp_pb2 as _ch_resp_pb
    import esp_rmaker_claim_pb2 as _claim_pb

    from utils import str_to_bytes as _str_to_bytes

    esp_prov = _esp_prov
    str_to_bytes = _str_to_bytes
    ch_resp_pb = _ch_resp_pb
    claim_pb = _claim_pb
    _bootstrapped = True

# RainMaker BLE provisioning service. esp_prov auto-discovers characteristics by
# their 0x2901 user description, so the custom `ch_resp` endpoint is reachable by
# name without listing it here.
DEFAULT_BLE_ADAPTER = 'hci0'

# Security2 username is fixed for RainMaker; the proof-of-possession doubles as the
# SRP6a password.
SEC2_USERNAME = 'wifiprov'

# Largest fragment the device sends on `rmaker_claim`; the CLI sends no larger.
CLAIM_FRAGMENT_SIZE = 200

# The claim service rejects a larger CSR (docs/en/specs/assisted-claiming.md §3.1).
CLAIM_CSR_MAX_LEN = 8192


async def discover_devices(name_prefix=None, adapter=DEFAULT_BLE_ADAPTER):
    """Scan for advertising BLE devices.

    Returns a list of (name, address) tuples for devices that advertise a name,
    filtered by ``name_prefix`` when provided. Done directly via bleak so app_sim
    can drive its own interactive selection (esp_prov's built-in picker is coupled
    to the connect step).
    """
    import bleak

    discovery = await bleak.BleakScanner.discover(return_adv=True, adapter=adapter)
    results = []
    for dev, _adv in discovery.values():
        name = dev.name
        if not name:
            continue
        if name_prefix and not name.startswith(name_prefix):
            continue
        results.append((name, dev.address))
    # Stable order for repeatable selection numbering.
    results.sort(key=lambda x: x[1])
    return results


async def connect(devname, adapter=DEFAULT_BLE_ADAPTER):
    """Open a BLE transport to the named device. Returns the transport object."""
    tp = await esp_prov.get_transport('ble', devname, adapter)
    if tp is None:
        raise RuntimeError(f'Failed to connect to BLE device {devname!r}')
    return tp


async def detect_secver(tp):
    """Auto-detect the protocomm security version from device capabilities.

    Reads ``prov.sec_ver`` from proto-ver; firmware without it falls back to esp_prov's ``no_sec`` capability check, which cannot tell sec1 from sec2. Returns (secver, sec_patch_ver).
    """
    try:
        secver = json.loads(await esp_prov.get_version(tp))['prov']['sec_ver']
    except (ValueError, KeyError, TypeError):
        if not await esp_prov.has_capability(tp):
            raise RuntimeError(
                'Security capabilities could not be determined from the device; '
                'cannot auto-detect security version'
            ) from None
        secver = int(not await esp_prov.has_capability(tp, 'no_sec'))
    sec_patch_ver = 0
    if secver == 2:
        sec_patch_ver = await esp_prov.get_sec_patch_ver(tp)
    return secver, sec_patch_ver


def make_security(secver, sec_patch_ver, pop=''):
    """Build the esp_prov security context for the resolved version.

    sec0: no credentials. sec1: PoP. sec2: username fixed to 'wifiprov', PoP used as
    the SRP6a password.
    """
    if secver == 0:
        return esp_prov.get_security(0, sec_patch_ver, '', '', '')
    if secver == 1:
        return esp_prov.get_security(1, sec_patch_ver, '', '', pop)
    if secver == 2:
        return esp_prov.get_security(2, sec_patch_ver, SEC2_USERNAME, pop, '')
    raise ValueError(f'Unsupported security version: {secver}')


async def establish_session(tp, sec):
    """Run the protocomm secure-session handshake. Returns True on success."""
    return bool(await esp_prov.establish_session(tp, sec))


async def ch_resp_get_signed(tp, sec, challenge):
    """Send the cloud challenge to the device's `ch_resp` endpoint and return its
    signed response.

    Builds a TypeCmdChallengeResponse RMakerChRespPayload carrying the challenge
    bytes, encrypts it with the session security context, writes it to the `ch_resp`
    characteristic, then decrypts and parses the RespCRPayload.

    Returns (signature_hex, node_id). The cloud /verify endpoint expects the
    signature in the same hex form that test_device.sign_challenge produces, so the
    raw signature bytes from RespCRPayload.payload are hex-encoded here.
    """
    if isinstance(challenge, str):
        challenge = challenge.encode()

    cmd = ch_resp_pb.RMakerChRespPayload()
    cmd.msg = ch_resp_pb.TypeCmdChallengeResponse
    cmd.cmdChallengeResponsePayload.payload = challenge

    enc = sec.encrypt_data(cmd.SerializeToString())
    resp_str = await tp.send_data('ch_resp', enc.decode('latin-1'))
    plain = sec.decrypt_data(str_to_bytes(resp_str))

    resp = ch_resp_pb.RMakerChRespPayload()
    resp.ParseFromString(plain)

    if resp.status != ch_resp_pb.Success:
        raise RuntimeError(
            f'ch_resp returned status {ch_resp_pb.RMakerChRespStatus.Name(resp.status)}'
        )

    sig_bytes = bytes(resp.respChallengeResponsePayload.payload)
    node_id = resp.respChallengeResponsePayload.node_id
    return sig_bytes.hex(), node_id


async def has_claim_capability(tp):
    """Report whether the device advertises assisted claiming (app-info ``rmaker`` cap ``claim``)."""
    try:
        info = json.loads(await esp_prov.get_version(tp))
    except (ValueError, TypeError):
        return False
    return 'claim' in info.get('rmaker', {}).get('cap', [])


async def _claim_request(tp, sec, msg, payload=b'', offset=0, total=None):
    """Send one `rmaker_claim` command and return the response's PayloadBuf. Raises on a non-Success status."""
    cmd = claim_pb.RMakerClaimPayload()
    cmd.msg = msg
    cmd.cmdPayload.Offset = offset
    cmd.cmdPayload.Payload = payload
    cmd.cmdPayload.TotalLen = len(payload) if total is None else total

    enc = sec.encrypt_data(cmd.SerializeToString())
    resp_str = await tp.send_data('rmaker_claim', enc.decode('latin-1'))
    resp = claim_pb.RMakerClaimPayload()
    resp.ParseFromString(sec.decrypt_data(str_to_bytes(resp_str)))

    name = claim_pb.RMakerClaimMsgType.Name(msg)
    if resp.msg != msg + 1:
        raise RuntimeError(f'rmaker_claim {name} got response type {resp.msg}')
    if resp.respPayload.Status != claim_pb.Success:
        raise RuntimeError(
            f'rmaker_claim {name} returned status {claim_pb.RMakerClaimStatus.Name(resp.respPayload.Status)}'
        )
    return resp.respPayload.Buf


async def claim_start(tp, sec):
    """Start claiming and return the claim-init request: ``{"mac_addr": ..., "platform": ...}``."""
    buf = await _claim_request(tp, sec, claim_pb.TypeCmdClaimStart)
    start = json.loads(bytes(buf.Payload))
    if not isinstance(start, dict) or not isinstance(start.get('mac_addr'), str) or not start['mac_addr']:
        raise RuntimeError(f'rmaker_claim start response has no mac_addr: {start!r}')
    return start


async def claim_init(tp, sec, node_id):
    """Hand the device its node ID and return its validated ``(csr, capabilities)`` from ``{"csr": ..., "send_mqtt_host": true, "capabilities": [...]}``."""
    data = b''
    buf = await _claim_request(tp, sec, claim_pb.TypeCmdClaimInit, json.dumps({'node_id': node_id}).encode())
    while True:
        if buf.Offset != len(data):
            raise RuntimeError(f'rmaker_claim CSR fragment at offset {buf.Offset}, expected {len(data)}')
        data += bytes(buf.Payload)
        if len(data) >= buf.TotalLen:
            return _validate_claim_verify_request(json.loads(data), node_id)
        buf = await _claim_request(tp, sec, claim_pb.TypeCmdClaimInit)


def _validate_claim_verify_request(req, node_id):
    """Check the device's verify request before it reaches the cloud. Returns ``(csr_pem, capabilities)``; capabilities is None when absent."""
    if not isinstance(req, dict):
        raise RuntimeError(f'rmaker_claim verify request is not a JSON object: {req!r}')

    csr_pem = req.get('csr')
    if not isinstance(csr_pem, str) or not csr_pem:
        raise RuntimeError('rmaker_claim verify request has no csr')
    if len(csr_pem) > CLAIM_CSR_MAX_LEN:
        raise RuntimeError(f'Device CSR is {len(csr_pem)} bytes, over the {CLAIM_CSR_MAX_LEN}-byte limit')
    try:
        csr = x509.load_pem_x509_csr(csr_pem.encode())
    except ValueError as e:
        raise RuntimeError(f'Device CSR is not a valid PEM CSR: {e}') from e
    if not csr.is_signature_valid:
        raise RuntimeError('Device CSR self-signature does not verify')
    key = csr.public_key()
    if not isinstance(key, ec.EllipticCurvePublicKey) or not isinstance(key.curve, ec.SECP256R1):
        raise RuntimeError('Device CSR key is not ECDSA P-256')
    # The server ignores the subject, but a CN other than node_id means the device did not adopt it.
    cns = csr.subject.get_attributes_for_oid(x509.NameOID.COMMON_NAME)
    if not cns or cns[0].value != node_id:
        cn = cns[0].value if cns else None
        raise RuntimeError(f'Device CSR CN is {cn!r}, expected node ID {node_id!r}')

    capabilities = req.get('capabilities')
    if capabilities is not None and (
            not isinstance(capabilities, list) or not all(isinstance(c, str) and c for c in capabilities)):
        raise RuntimeError(f'rmaker_claim capabilities must be a list of non-empty strings: {capabilities!r}')
    return csr_pem, capabilities


async def claim_verify(tp, sec, certificate, mqtt_host):
    """Send the signed certificate and MQTT host to the device, which persists them on the last fragment."""
    data = json.dumps({'certificate': certificate, 'mqtt_host': mqtt_host}).encode()
    for offset in range(0, len(data), CLAIM_FRAGMENT_SIZE):
        await _claim_request(tp, sec, claim_pb.TypeCmdClaimVerify,
                             data[offset:offset + CLAIM_FRAGMENT_SIZE], offset, len(data))


async def claim_abort(tp, sec):
    """Reset the device's claim state; errors are ignored since this runs on a failure path."""
    try:
        await _claim_request(tp, sec, claim_pb.TypeCmdClaimAbort)
    except Exception:
        pass


async def send_wifi(tp, sec, ssid, passphrase):
    """Set, apply, and wait for the device to connect to Wi-Fi.

    Returns True only if the device reports a successful connection.
    """
    if not await esp_prov.send_wifi_config(tp, sec, ssid, passphrase):
        raise RuntimeError('Failed to send Wi-Fi config to device')
    if not await esp_prov.apply_wifi_config(tp, sec):
        raise RuntimeError('Failed to apply Wi-Fi config on device')
    return bool(await esp_prov.wait_wifi_connected(tp, sec))


async def disconnect(tp):
    try:
        await tp.disconnect()
    except Exception:
        pass
