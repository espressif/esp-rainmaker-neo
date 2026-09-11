# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Stable import path for the RainMaker Neo Python SDK.

The code lives in :mod:`esp_morpheus.sdk`; every module here binds the matching one there, except
`test_mcp`, whose client only the integration tests use and which stays here. Other repositories
clone this repo and import these names, so the path is supported, not transitional.

Because this file's own location identifies the checkout, it puts cli/src on `sys.path` when
esp-morpheus is not installed, and pins the repo root, which the package otherwise infers from the
CWD.

    py_sdk.test_user        -> esp_morpheus.sdk.user
    py_sdk.test_device      -> esp_morpheus.sdk.device
    py_sdk.test_group       -> esp_morpheus.sdk.group
    py_sdk.test_matter      -> esp_morpheus.sdk.matter
    py_sdk.test_util        -> esp_morpheus.sdk.util
    py_sdk.test_smartthings -> esp_morpheus.sdk.smartthings
"""

import importlib.util
import pathlib
import sys

_REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent

if importlib.util.find_spec('esp_morpheus') is None:
    sys.path.insert(0, str(_REPO_ROOT / 'cli' / 'src'))

from esp_morpheus import paths as _paths  # noqa: E402

_paths.set_repo_root(_REPO_ROOT)
