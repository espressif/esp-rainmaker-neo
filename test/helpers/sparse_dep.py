# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
sparse_dep.py — Vendor a single subdirectory of a Git repo at runtime.

A lightweight alternative to git submodules for when you only need one folder
out of a large repository and want it:
  * synced automatically when the script runs (like `git submodule update`),
  * pinned to a tag / branch / commit,
  * skipped entirely (no network) when already present at the desired ref.

Example
-------
    from sparse_dep import SparseDependency

    SparseDependency(
        repo="https://github.com/espressif/idf-extra-components.git",
        subdir="network_provisioning/tool/esp_prov",
        ref="master",   # tag, branch, or commit SHA
    ).add_to_path()          # syncs if needed, inserts dir on sys.path
    import esp_prov          # now importable
"""

from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path
from typing import Iterable, List, Optional, Union


class SparseDependency:
    """Fetch one or more subdirectories of a Git repo via sparse + shallow checkout.

    Parameters
    ----------
    repo:
        Clone URL of the repository.
    subdir:
        Path (or list of paths), relative to the repo root, to check out.
        Only these directories are fetched; the rest of the repo is not.
    ref:
        Git ref to pin to — a tag, branch, or commit SHA. Tags/branches always
        work with a shallow (depth=1) fetch; arbitrary SHAs work only if the
        server allows fetching them directly (GitHub generally does).
    dest:
        Where to place the checkout. Defaults to ``.vendor/<repo-name>``.
    import_subdir:
        Directory (relative to the checkout root) to return / put on sys.path.
        Defaults to the first ``subdir``. Override when the importable package
        lives at a different level than the sparse path.
    depth:
        Fetch depth (default 1 = shallow).
    git:
        git executable name/path (default "git").
    """

    def __init__(
        self,
        repo: str,
        subdir: Union[str, Iterable[str]],
        ref: str = "main",
        *,
        dest: Optional[Union[str, Path]] = None,
        import_subdir: Optional[str] = None,
        depth: int = 1,
        git: str = "git",
    ) -> None:
        self.repo = repo
        self.subdirs: List[str] = [subdir] if isinstance(subdir, str) else list(subdir)
        if not self.subdirs:
            raise ValueError("at least one subdir is required")
        self.ref = ref
        self.depth = depth
        self.git = git

        repo_stem = repo.rstrip("/").rsplit("/", 1)[-1]
        if repo_stem.endswith(".git"):
            repo_stem = repo_stem[:-4]
        self.dest = Path(dest) if dest else Path(".vendor") / repo_stem

        rel_import = import_subdir if import_subdir is not None else self.subdirs[0]
        self._import_dir = self.dest / rel_import
        self._stamp = self.dest / ".sparse_dep_ref"

    # ---- public API -------------------------------------------------------

    @property
    def import_dir(self) -> Path:
        """The directory a caller would add to sys.path to import the package."""
        return self._import_dir

    def is_synced(self) -> bool:
        """True if the desired ref is already checked out (cheap, no network)."""
        return (
            self._stamp.exists()
            and self._stamp.read_text().strip() == self.ref
            and self._import_dir.exists()
        )

    def ensure(self, update: bool = False) -> Path:
        """Sync the subdir(s) to ``ref`` if needed; return the import directory.

        Set ``update=True`` to force a re-fetch even when the local stamp already
        matches ``ref`` — useful when ``ref`` is a mutable branch and you want the
        latest commit on it.
        """
        if not update and self.is_synced():
            return self._import_dir

        self._check_git()
        self.dest.mkdir(parents=True, exist_ok=True)

        if not (self.dest / ".git").exists():
            self._run("init", str(self.dest), cwd=None)
        self._ensure_remote()

        # Restrict the working tree to just the requested directories.
        self._git("sparse-checkout", "init", "--cone")
        self._git("sparse-checkout", "set", *self.subdirs)

        # Shallow-fetch only the pinned ref and check it out (detached HEAD).
        self._git("fetch", "--depth", str(self.depth), "origin", self.ref)
        self._git("checkout", "FETCH_HEAD")

        if not self._import_dir.exists():
            raise FileNotFoundError(
                f"expected import dir not found after checkout: {self._import_dir}\n"
                f"check that subdir / import_subdir match the repository layout"
            )

        # gitignore the imported directory
        (self._import_dir / ".gitignore").write_text("*\n")
        # stamp the ref
        self._stamp.write_text(self.ref)

        return self._import_dir

    def add_to_path(self, update: bool = False, front: bool = True) -> Path:
        """Ensure the dependency, place its dir on ``sys.path``, and return it.

        ``front=True`` (default) gives it import priority over installed packages.
        """
        path = self.ensure(update=update)
        s = str(path.resolve())
        if s in sys.path:
            sys.path.remove(s)
        if front:
            sys.path.insert(0, s)
        else:
            sys.path.append(s)
        return path

    def current_commit(self) -> Optional[str]:
        """Return the checked-out commit SHA, or None if not yet synced."""
        if not (self.dest / ".git").exists():
            return None
        out = self._git("rev-parse", "HEAD", capture=True)
        return out.strip() if out else None

    # ---- internals --------------------------------------------------------

    def _check_git(self) -> None:
        if shutil.which(self.git) is None:
            raise RuntimeError(f"'{self.git}' not found on PATH; git is required")

    def _ensure_remote(self) -> None:
        remotes = (self._git("remote", capture=True) or "").split()
        if "origin" in remotes:
            self._git("remote", "set-url", "origin", self.repo)
        else:
            self._git("remote", "add", "origin", self.repo)

    def _git(self, *args: str, capture: bool = False) -> Optional[str]:
        return self._run(*args, cwd=self.dest, capture=capture)

    def _run(self, *args: str, cwd: Optional[Path], capture: bool = False) -> Optional[str]:
        cmd = [self.git]
        if cwd is not None:
            cmd += ["-C", str(cwd)]
        cmd += list(args)
        if capture:
            return subprocess.run(cmd, check=True, text=True, capture_output=True).stdout
        subprocess.run(cmd, check=True)
        return None
