# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Stable alias for :mod:`esp_morpheus.outputs`, where the code now lives.

The CDK apps and the pytest suite import this path, so it is supported rather than transitional.
Binding the module object itself, rather than re-exporting its names, keeps the two paths the same
module: one class object, so isinstance holds across them, and private names stay reachable.

Because this file's own location identifies the checkout, it pins the repo root before esp_morpheus
reads it; the package otherwise infers the root from the CWD.
"""

import os
import sys

from esp_morpheus import paths as _paths

_paths.set_repo_root(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from esp_morpheus.outputs import *  # noqa: E402,F401,F403
import esp_morpheus.outputs as _target  # noqa: E402

sys.modules[__name__] = _target
