# SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# English language Sphinx config.
#
# Uses ../conf_common.py for all non-language-specific settings.

try:
    from conf_common import *
except ImportError:
    import os
    import sys

    sys.path.insert(0, os.path.abspath("../"))
    from conf_common import *

# -- Project information -----------------------------------------------------

project = "ESP RainMaker Neo Cloud Specifications"
copyright = "2026, Espressif Systems (Shanghai) CO., LTD"
author = "Espressif Systems"

language = "en"
