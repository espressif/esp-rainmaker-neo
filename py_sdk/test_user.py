# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Stable alias for :mod:`esp_morpheus.sdk.user`, where the code now lives.

Binding the module object itself, rather than re-exporting its names, keeps the two paths the same
module: one class object, so isinstance holds across them, and private names stay reachable.
"""

import sys

import esp_morpheus.sdk.user as _target
from esp_morpheus.sdk.user import *  # noqa: F401,F403

sys.modules[__name__] = _target
