#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Register a fresh bridge node through the production admin REST API.

Generates a key+cert locally, authenticates as an admin user from
test_config.json, and POSTs to /v1/admin/nodes with
`capabilities: ["bridge"]`. Saves the cert + key under
.bridges/<thing_name>/ so test/bridge_sim.py can drive the new bridge
immediately (bridges_config.json is updated with the new entry).

Usage:
    python3 tools/bridge_register.py my-bridge-001 \\
        [--user 0] [--tags tag1,tag2] [--admin-group-name homeA]

Requires test_config.json + rmng-outputs.json in the working directory.
"""

import argparse
import json
import sys
import urllib.request
from pathlib import Path
from types import SimpleNamespace


RMNG_OUTPUTS_PATH = Path("rmng-outputs.json")
TEST_CONFIG_PATH = Path("test_config.json")
BRIDGES_DIR = Path(".bridges")
BRIDGES_CONFIG = BRIDGES_DIR / "bridges_config.json"
ROOT_CA_PATH = BRIDGES_DIR / "AmazonRootCA1.pem"
AMAZON_ROOT_CA_URL = "https://www.amazontrust.com/repository/AmazonRootCA1.pem"


def _load_outputs() -> dict:
    if not RMNG_OUTPUTS_PATH.exists():
        sys.exit(f"ERROR: {RMNG_OUTPUTS_PATH} not found. Run `make deploy-rmng` first.")
    with open(RMNG_OUTPUTS_PATH) as f:
        return json.load(f)


def _load_test_config() -> dict:
    if not TEST_CONFIG_PATH.exists():
        sys.exit(f"ERROR: {TEST_CONFIG_PATH} not found.")
    with open(TEST_CONFIG_PATH) as f:
        return json.load(f)


def _pick_admin_user(test_config: dict, selector) -> dict:
    """Resolve --user (index or username) to a test_config users[] entry
    that has super_admin: true."""
    users = test_config.get("users", [])
    if selector is None:
        for u in users:
            if u.get("super_admin"):
                return u
        sys.exit("ERROR: no super_admin user in test_config.json")
    try:
        idx = int(selector)
        if 0 <= idx < len(users):
            return users[idx]
        sys.exit(f"ERROR: user index {idx} out of range")
    except ValueError:
        for u in users:
            if u.get("name") == selector:
                return u
        sys.exit(f"ERROR: user {selector!r} not found in test_config.json")


def _build_admin_user(outputs: dict, user_cfg: dict):
    from test.test_user import User

    rmng_base = outputs.get("rmng-base", {})
    espuser_base = outputs.get("espuser-base", {})
    espuser_core = outputs.get("espuser-core", {})

    region = rmng_base["StackRegion"]
    identity_pool_id = rmng_base["IdentityPoolId"]
    api_gateway_url = rmng_base["ApiGatewayUrl"]
    iot_endpoint = rmng_base["IoTEndpointUrl"]
    admin_user_pool_id = espuser_base.get("EspAdminUserPoolId") or rmng_base.get("AdminUserPoolId", "")
    admin_client_id = espuser_base.get("EspAdminUserPoolClientId") or rmng_base.get("AdminUserPoolClientId", "")
    user_api_gateway_url = espuser_base.get("EspUserApiUrl", "")
    register_user_lambda_arn = espuser_core.get("UserFunctionArn")

    user = User(
        user_cfg["name"],
        user_cfg["password"],
        region,
        admin_user_pool_id,
        admin_client_id,
        identity_pool_id,
        api_gateway_url,
        user_api_gateway_url,
        iot_endpoint,
        register_user_lambda_arn,
        is_super_admin=True,
    )
    if not user.get_aws_credentials():
        sys.exit("ERROR: failed to authenticate admin user against Cognito")
    return user, region, iot_endpoint


def _download_root_ca() -> Path:
    if ROOT_CA_PATH.exists():
        return ROOT_CA_PATH
    ROOT_CA_PATH.parent.mkdir(parents=True, exist_ok=True)
    with urllib.request.urlopen(AMAZON_ROOT_CA_URL) as resp:
        ROOT_CA_PATH.write_bytes(resp.read())
    return ROOT_CA_PATH


def _save_bridge_artifacts(*, thing_name: str, key_pem: str, combined_cert_pem: str,
                           iot_endpoint: str, region: str) -> dict:
    """Write cert+key to .bridges/<thing>/ and append an entry to
    bridges_config.json so test/bridge_sim.py can pick the bridge up by
    name."""
    thing_dir = BRIDGES_DIR / thing_name
    thing_dir.mkdir(parents=True, exist_ok=True)
    (thing_dir / "cert.pem").write_text(combined_cert_pem)
    (thing_dir / "private.key").write_text(key_pem)
    (thing_dir / "private.key").chmod(0o600)

    root_ca = _download_root_ca()
    cert_entry = {
        "thing_name": thing_name,
        "cert_pem_path": str(thing_dir / "cert.pem"),
        "private_key_path": str(thing_dir / "private.key"),
    }

    if BRIDGES_CONFIG.exists():
        cfg = json.loads(BRIDGES_CONFIG.read_text())
    else:
        cfg = {
            "iot_endpoint": iot_endpoint,
            "region": region,
            "root_ca": str(root_ca),
            "certs": {},
        }
    cfg.setdefault("certs", {})[thing_name] = cert_entry
    BRIDGES_CONFIG.write_text(json.dumps(cfg, indent=2))
    return cert_entry


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("thing_name", help="Name of the new bridge to register.")
    parser.add_argument(
        "--user", default=None,
        help="test_config.json users[] index or name (default: first super_admin user).",
    )
    parser.add_argument(
        "--tags", default="",
        help="Comma-separated tags to apply at registration.",
    )
    parser.add_argument(
        "--admin-group-name", default=None,
        help="admin_parent_group_name to register the bridge under "
             "(creates the admin group if missing).",
    )
    args = parser.parse_args()

    # PYTHONPATH must include the repo root for `test.*` imports; run
    # from the repo root (the standard for everything else in this repo).
    sys.path.insert(0, str(Path.cwd()))
    from test.test_device import generate_key_and_cert

    outputs = _load_outputs()
    test_config = _load_test_config()
    user_cfg = _pick_admin_user(test_config, args.user)
    user, region, iot_endpoint = _build_admin_user(outputs, user_cfg)

    thing_name = args.thing_name
    tags = [t.strip() for t in args.tags.split(",") if t.strip()]

    print(f"  [generate] key+cert for {thing_name}")
    key_pem, combined_cert_pem = generate_key_and_cert(thing_name)
    device = SimpleNamespace(
        node_thing_name=thing_name,
        node_cert=combined_cert_pem.split("-----END CERTIFICATE-----\n", 1)[0] + "-----END CERTIFICATE-----\n",
        node_ca_cert=combined_cert_pem.split("-----END CERTIFICATE-----\n", 1)[1] if "-----END CERTIFICATE-----\n" in combined_cert_pem else "",
    )

    print(f"  [register] POST /v1/admin/nodes  capabilities=['bridge']  "
          f"tags={tags}  admin_group={args.admin_group_name!r}")
    ok = user.register_node(
        device,
        tags=tags or None,
        admin_parent_group_name=args.admin_group_name,
        capabilities=["bridge"],
    )
    if not ok:
        sys.exit("ERROR: registration failed (see preceding log lines)")
    print(f"  [ok] {thing_name} registered as bridge")

    entry = _save_bridge_artifacts(
        thing_name=thing_name,
        key_pem=key_pem,
        combined_cert_pem=combined_cert_pem,
        iot_endpoint=iot_endpoint,
        region=region,
    )
    print(f"  [saved] cert at {entry['cert_pem_path']}")
    print(f"  [saved] key  at {entry['private_key_path']}")
    print()
    print("Next step:")
    print(f"  python3 test/bridge_sim.py --bridge {thing_name}")


if __name__ == "__main__":
    main()
