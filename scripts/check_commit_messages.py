#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Reject commit messages that carry a Co-authored-by trailer.

    python3 scripts/check_commit_messages.py --since "$CI_MERGE_REQUEST_DIFF_BASE_SHA"
    python3 scripts/check_commit_messages.py --since origin/main

Authorship in this repository is the committer alone, so the gate runs on every MR commit.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys

COAUTHOR_RE = re.compile(r"^\s*co-authored-by\s*:", re.IGNORECASE)

# Commit records are separated by a NUL so a body may hold blank lines or any text.
LOG_FORMAT = "%H%x00%s%x00%B%x00"


def resolve(ref: str) -> str:
    """Full SHA of ref. An empty ref would make the range "..HEAD", which git reads as HEAD..HEAD and so passes every commit unchecked."""
    if not ref.strip():
        sys.exit("--since is empty: the base commit was not provided (is CI_MERGE_REQUEST_DIFF_BASE_SHA set?)")
    result = subprocess.run(["git", "rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}"], capture_output=True, text=True)
    if result.returncode != 0:
        sys.exit(f"--since {ref!r} does not name a commit in this clone")
    return result.stdout.strip()


def commits_since(ref: str) -> list[tuple[str, str, str]]:
    out = subprocess.run(
        ["git", "log", f"--format={LOG_FORMAT}", f"{ref}..HEAD"],
        check=True, capture_output=True, text=True,
    ).stdout
    fields = out.split("\0")
    records = []
    for i in range(0, len(fields) - 1, 3):
        sha, subject, body = fields[i].strip(), fields[i + 1], fields[i + 2]
        if sha:
            records.append((sha, subject, body))
    return records


def offending_lines(body: str) -> list[str]:
    return [line.strip() for line in body.splitlines() if COAUTHOR_RE.match(line)]


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--since", required=True, metavar="REF", help="check the commits in REF..HEAD")
    args = ap.parse_args()

    bad = [(sha, subject, lines) for sha, subject, body in commits_since(resolve(args.since)) if (lines := offending_lines(body))]
    if not bad:
        return 0

    print(f"{len(bad)} commit(s) carry a Co-authored-by trailer, which this repository does not accept:", file=sys.stderr)
    for sha, subject, lines in bad:
        print(f"  {sha[:12]} {subject}", file=sys.stderr)
        for line in lines:
            print(f"      {line}", file=sys.stderr)
    print(
        "\nRemove the trailer and rewrite the commit (git commit --amend, or git rebase -i onto the target branch),"
        " then force-push the branch. Turn the trailer off in the tool that adds it so it does not come back.",
        file=sys.stderr,
    )
    return 1


if __name__ == "__main__":
    sys.exit(main())
