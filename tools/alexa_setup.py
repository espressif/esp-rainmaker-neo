#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Stable alias for :mod:`esp_morpheus.sdk.alexa_setup`, where the code now lives.

Binding the module object itself, rather than re-exporting its names, keeps the two paths the same
module: one class object, so isinstance holds across them, and private names stay reachable.
"""

import sys

from esp_morpheus.sdk.alexa_setup import *  # noqa: F401,F403
import esp_morpheus.sdk.alexa_setup as _target

if __name__ == "__main__":
    _target.main()
else:
    sys.modules[__name__] = _target
