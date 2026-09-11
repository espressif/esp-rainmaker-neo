# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Credential-blob resolution for the itest suite.

Each blob (Mailosaur creds, APNs/Firebase push creds) resolves from three sources,
first hit wins:

1. this repo's copy under ``test/`` — gitignored; what a developer drops in by hand
2. the matching ``RMNG_*_JSON`` env var holding the same JSON inline — one masked
   variable per blob in CI
3. the same relative path under the superproject's ``test/`` dir, when this repo is
   checked out as a submodule beside one (``../test`` by default)

(3) exists so enterprise developers keep their credentials in the superproject and
never copy secrets into this checkout. The superproject mirrors this repo's layout
under ``test/``, so one relative path addresses both. An open-source checkout has no
sibling, the lookup misses, and resolution is unchanged.

Detection is by presence — the same convention the Makefile uses for
``../addon_modules``. Core names no specific superproject and requires none.
"""
import json
import os

# This file is test/itest/config_sources.py, so two levels up is this repo's test/ dir.
_REPO_TEST_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# Superproject's test/ dir — sibling of this repo's root. Overridable for a
# non-default checkout layout.
SUPERPROJECT_TEST_DIR = os.environ.get("RMNG_SUPERPROJECT_TEST_DIR") or os.path.join(
    _REPO_TEST_DIR, os.pardir, os.pardir, "test"
)


def repo_path(rel_path: str) -> str:
    """Absolute path to ``rel_path`` under this repo's test/ dir."""
    return os.path.join(_REPO_TEST_DIR, rel_path)


def superproject_path(rel_path: str) -> str:
    """Absolute path to ``rel_path`` under the superproject's test/ dir."""
    return os.path.join(SUPERPROJECT_TEST_DIR, rel_path)


def _read_json_file(path: str):
    try:
        with open(path) as f:
            return json.load(f)
    except (FileNotFoundError, NotADirectoryError, OSError, json.JSONDecodeError):
        return None


def _read_json_env(env_var: str):
    blob = os.environ.get(env_var)
    if not blob:
        return None
    try:
        return json.loads(blob)
    except json.JSONDecodeError:
        return None


def load_json_config(rel_path: str, env_var: str) -> dict:
    """Resolve a credential blob; empty dict when no source supplies one.

    ``rel_path`` is relative to test/ (e.g. "itest/itest_config.json") and addresses
    both this repo and the superproject, which share that layout.
    """
    for candidate in (
        _read_json_file(repo_path(rel_path)),
        _read_json_env(env_var),
        _read_json_file(superproject_path(rel_path)),
    ):
        if isinstance(candidate, dict) and candidate:
            return candidate
    return {}


def describe_sources(rel_path: str, env_var: str) -> str:
    """Human-readable source list for 'not configured' messages."""
    return f"test/{rel_path}, {env_var} env, or {os.path.join('../test', rel_path)}"
