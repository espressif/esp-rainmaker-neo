# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""The in-repo scripts that drive a deployment through the SDK still load, from any directory.

Nothing else runs them, so a moved SDK module would otherwise break them unnoticed (#313).
"""

import subprocess
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]

SCRIPTS = [
    'tools/register_node.py',
    'tools/bridge_register.py',
    'test/helpers/test_admin_list_things.py',
    'test/helpers/ts_performance_test.py',
]


@pytest.mark.parametrize('script', SCRIPTS)
def test_script_loads_from_any_directory(script, tmp_path):
    # run_path executes the module body but not its __main__ block, so imports and import-time work run without a deployment.
    code = f'import runpy; runpy.run_path({str(REPO_ROOT / script)!r})'
    result = subprocess.run([sys.executable, '-c', code], cwd=tmp_path, capture_output=True, text=True)
    assert result.returncode == 0, result.stderr
