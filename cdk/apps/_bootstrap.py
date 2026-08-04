# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""sys.path setup shared by every CDK entry point in this directory.

Running `python3 cdk/apps/<app>.py` puts this directory at sys.path[0], so `import _bootstrap` resolves before anything else is importable — which is why each app imports it as its very first repo-local statement.
"""
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]

sys.path[:0] = [str(p) for p in (
    REPO_ROOT,                                      
    REPO_ROOT / "cdk" / "utils",                     # app_common, arn_utils
    REPO_ROOT / "src" / "esp-cloud-common" / "cdk_go",      # gsi_infra, from the git submodule
    REPO_ROOT.parent,                               # addon_modules/<pkg>; absent in open-source checkouts
    REPO_ROOT.parent / "addon_modules" / "test", 
)]
