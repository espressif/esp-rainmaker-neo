#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Register a fresh bridge node through the production admin REST API.

Generates a key+cert locally, authenticates as an admin user from
esp-morpheus's test_config.json, and POSTs to /v1/admin/nodes with
`capabilities: ["bridge"]`. Saves the cert + key under
<repo root>/.bridges/<thing_name>/ so test/bridge_sim.py can drive the new bridge
immediately (bridges_config.json is updated with the new entry).

Usage:
    python3 tools/bridge_register.py my-bridge-001 \\
        [--user 0] [--tags tag1,tag2] [--admin-group-name homeA]

Reads rmng-outputs.json from the repo root, so it runs from any directory.
"""

import argparse
import json
import sys
import urllib.request
from pathlib import Path
from types import SimpleNamespace

# Run as a script, so only tools/ is on sys.path; adding the repo root makes py_sdk and scripts importable from any directory.
REPO_ROOT = Path(__file__).resolve().parent.parent
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from py_sdk.test_device import (  # noqa: E402
    generate_key_and_cert,
    split_combined_cert_pem,
)
from py_sdk.test_user import User  # noqa: E402
from scripts.rmng_outputs import TEST_CONFIG_PATH, RmngSettings  # noqa: E402

BRIDGES_DIR = REPO_ROOT / ".bridges"
BRIDGES_CONFIG = BRIDGES_DIR / "bridges_config.json"
ROOT_CA_PATH = BRIDGES_DIR / "AmazonRootCA1.pem"
AMAZON_ROOT_CA_URL = "https://www.amazontrust.com/repository/AmazonRootCA1.pem"


def _load_settings() -> RmngSettings:
    try:
        return RmngSettings.from_source()
    except FileNotFoundError as e:
        sys.exit(f"ERROR: {e.filename} not found. Run `make deploy` first.")


def _load_test_config() -> dict:
    try:
        with open(TEST_CONFIG_PATH) as f:
            return json.load(f)
    except FileNotFoundError:
        sys.exit(f"ERROR: {TEST_CONFIG_PATH} not found. Run `esp-morpheus admin test-data setup` first.")


def _pick_admin_user(test_config: dict, selector) -> dict:
    """Resolve --user (index or username) to a test_config users[] entry; the default is the first with admin: true."""
    users = test_config.get("users", [])
    if selector is None:
        for u in users:
            if u.get("admin"):
                return u
        sys.exit(f"ERROR: no admin user in {TEST_CONFIG_PATH}")
    try:
        idx = int(selector)
        if 0 <= idx < len(users):
            return users[idx]
        sys.exit(f"ERROR: user index {idx} out of range")
    except ValueError:
        for u in users:
            if u.get("name") == selector:
                return u
        sys.exit(f"ERROR: user {selector!r} not found in {TEST_CONFIG_PATH}")


def _build_admin_user(settings: RmngSettings, user_cfg: dict) -> User:
    user = User.from_settings(settings, user_cfg["name"], user_cfg["password"], is_admin=True)
    if not user.get_aws_credentials():
        sys.exit("ERROR: failed to authenticate admin user against Cognito")
    return user


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
        help="test_config.json users[] index or name (default: first admin user).",
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

    settings = _load_settings()
    test_config = _load_test_config()
    user_cfg = _pick_admin_user(test_config, args.user)
    user = _build_admin_user(settings, user_cfg)

    thing_name = args.thing_name
    tags = [t.strip() for t in args.tags.split(",") if t.strip()]

    print(f"  [generate] key+cert for {thing_name}")
    key_pem, combined_cert_pem = generate_key_and_cert(thing_name)
    node_cert, node_ca_cert = split_combined_cert_pem(combined_cert_pem)
    device = SimpleNamespace(node_thing_name=thing_name, node_cert=node_cert, node_ca_cert=node_ca_cert)

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
        iot_endpoint=settings.iot_endpoint,
        region=settings.region,
    )
    print(f"  [saved] cert at {entry['cert_pem_path']}")
    print(f"  [saved] key  at {entry['private_key_path']}")
    print()
    print("Next step:")
    print(f"  python3 test/bridge_sim.py --bridge {thing_name}")


if __name__ == "__main__":
    main()
