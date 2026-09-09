# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Where morpheus keeps its config, history and simulator caches.

Installed, the command runs from any directory, so nothing may anchor to the CWD. Each resolver
below answers in the same order: an environment override, then the repo checkout when the caller
is standing in one, then XDG. The checkout comes before XDG so a developer who already has
cli/test_config.json and a .sim/ cache keeps using them after the install.
"""

import os
import pathlib

# Marker files that identify a rmng checkout. Both must be present, so a directory that merely
# happens to contain a cli/ folder is not mistaken for one.
_REPO_MARKERS = ('cdk.json', 'cli/src/esp_morpheus')

_ENV_CONFIG = 'MORPHEUS_CONFIG'
_ENV_CONFIG_DIR = 'MORPHEUS_CONFIG_DIR'
_ENV_CACHE_DIR = 'MORPHEUS_CACHE_DIR'


def _detect_repo_root():
    """The rmng checkout this process belongs to, or None.

    An installed wheel lives outside the repo, so the package's own location says nothing; walk up
    from the CWD instead. MORPHEUS_REPO_ROOT overrides for a caller that knows better.
    """
    override = os.environ.get('MORPHEUS_REPO_ROOT')
    if override:
        return pathlib.Path(override).expanduser().resolve()

    for candidate in (pathlib.Path.cwd().resolve(), *pathlib.Path.cwd().resolve().parents):
        if all((candidate / marker).exists() for marker in _REPO_MARKERS):
            return candidate
    return None


_repo_root = _detect_repo_root()


def repo_root():
    """The enclosing rmng checkout, or None when running from an installed wheel."""
    return _repo_root


def set_repo_root(path):
    """Override the detected checkout; used by the in-repo shims, which know where they live."""
    global _repo_root
    _repo_root = pathlib.Path(path).resolve() if path else None


def _xdg(env_var, default):
    base = os.environ.get(env_var)
    return pathlib.Path(base).expanduser() if base else pathlib.Path.home() / default


def config_dir():
    """Directory for user-editable state: test_config.json, bot credentials."""
    override = os.environ.get(_ENV_CONFIG_DIR)
    if override:
        return pathlib.Path(override).expanduser()
    if _repo_root:
        return _repo_root / 'cli'
    return _xdg('XDG_CONFIG_HOME', '.config') / 'morpheus'


def cache_dir():
    """Directory for regenerable state: command history, simulator caches, vendored checkouts."""
    override = os.environ.get(_ENV_CACHE_DIR)
    if override:
        return pathlib.Path(override).expanduser()
    if _repo_root:
        return _repo_root
    return _xdg('XDG_CACHE_HOME', '.cache') / 'morpheus'


def ensure(path):
    """Create a directory and return it."""
    path = pathlib.Path(path)
    path.mkdir(parents=True, exist_ok=True)
    return path


def test_config_path():
    """The seeded test users and nodes, written by `morpheus test-data setup`."""
    override = os.environ.get(_ENV_CONFIG)
    if override:
        return pathlib.Path(override).expanduser()
    return config_dir() / 'test_config.json'


def bot_credentials_path():
    """IAM access keys for the CI bot user, written by `morpheus bot-user create`."""
    return config_dir() / 'bot-iam-user-credentials.json'


def history_path(name):
    """prompt_toolkit history for one REPL context (user, device, app_sim, device_sim)."""
    if _repo_root:
        return _repo_root / 'cli' / f'.{name}.command_history'
    return ensure(cache_dir()) / f'{name}.command_history'


def sim_cache_dir(name):
    """Per-simulator scratch directory (node state, shadow snapshots)."""
    return ensure(cache_dir() / '.sim' / name) if _repo_root else ensure(cache_dir() / 'sim' / name)


def vendor_dir():
    """Where sparse_dep clones esp_prov and friends. Never CWD-relative."""
    return ensure(cache_dir() / '.vendor') if _repo_root else ensure(cache_dir() / 'vendor')


def data_dir():
    """Packaged JSON shipped with the wheel: node configs, tags, config examples."""
    return pathlib.Path(__file__).parent / 'data'


def resolve_data(relative):
    """Resolve a data path from disk first, then from the packaged data directory.

    test_config.json records paths like ``cli/cli_data/node_config_va_multi.json``, which exist in a
    checkout but not in an install, so the packaged copy is the fallback rather than the primary.
    """
    candidate = pathlib.Path(relative).expanduser()
    if candidate.is_absolute() and candidate.exists():
        return candidate
    if _repo_root and (_repo_root / candidate).exists():
        return _repo_root / candidate
    if candidate.exists():
        return candidate
    return data_dir() / pathlib.Path(relative).name
