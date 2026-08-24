#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Check (or insert) the SPDX licence header on source files.

    python3 scripts/check_license_headers.py --added-since origin/main
    python3 scripts/check_license_headers.py --all
    python3 scripts/check_license_headers.py --added-since origin/main --fix
"""

from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys

COPYRIGHT = "SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD"
LICENSE_ID = "SPDX-License-Identifier: Apache-2.0"

COPYRIGHT_RE = re.compile(
    r"SPDX-FileCopyrightText:\s*\d{4}(?:-\d{4})?\s+Espressif Systems \(Shanghai\) CO LTD"
)
LICENSE_RE = re.compile(r"SPDX-License-Identifier:\s*Apache-2\.0")

HEADER_SCAN_LINES = 12

HASH = ("# ", None, None)
SLASH = ("// ", None, None)
BLOCK = (" * ", "/*", " */")
XML = ("     ", "<!--", "-->")

STYLES: dict[str, tuple[str, str | None, str | None]] = {
    ".go": SLASH,
    ".proto": SLASH,
    ".groovy": SLASH,
    ".jenkinsfile": SLASH,
    ".java": SLASH,
    ".py": HASH,
    ".sh": HASH,
    ".bash": HASH,
    ".yml": HASH,
    ".yaml": HASH,
    ".toml": HASH,
    ".ini": HASH,
    ".cfg": HASH,
    ".ts": BLOCK,
    ".tsx": BLOCK,
    ".js": BLOCK,
    ".jsx": BLOCK,
    ".mjs": BLOCK,
    ".cjs": BLOCK,
    ".css": BLOCK,
    ".scss": BLOCK,
    ".html": XML,
    ".svg": XML,
}

NAME_STYLES: dict[str, tuple[str, str | None, str | None]] = {
    "Makefile": HASH,
    "Dockerfile": HASH,
    "Jenkinsfile": SLASH,
}

EXCLUDED_PREFIXES = (
    "esp-rainmaker-neo-enterprise/",
    "docs/api/swagger-ui",
    "docs/api/oauth2-redirect.html",
    "docs/api/index.css",
)

EXCLUDED_SUFFIXES = (
    "_pb2.py",
    "_pb2_grpc.py",
    ".pb.go",
    "_generated.go",
    ".gen.go",
    "-lock.json",
    ".min.js",
    ".min.css",
)

PRELUDE_RE = re.compile(
    r"^(#!|# -\*- coding|<\?xml|<!DOCTYPE|<!doctype|//\s*\+build|//go:build)"
)


def is_checkable(path: str) -> bool:
    if any(path.startswith(p) for p in EXCLUDED_PREFIXES):
        return False
    if any(path.endswith(s) for s in EXCLUDED_SUFFIXES):
        return False
    if style_for(path) is None:
        return False
    try:
        return os.path.getsize(path) > 0
    except OSError:
        return False


def style_for(path: str):
    name = os.path.basename(path)
    if name in NAME_STYLES:
        return NAME_STYLES[name]
    _, ext = os.path.splitext(name)
    return STYLES.get(ext.lower())


def has_header(path: str) -> bool:
    try:
        with open(path, encoding="utf-8", errors="replace") as fh:
            head = "".join(fh.readline() for _ in range(HEADER_SCAN_LINES))
    except OSError as exc:
        print(f"{path}: cannot read: {exc}", file=sys.stderr)
        return False
    return bool(COPYRIGHT_RE.search(head) and LICENSE_RE.search(head))


def header_block(path: str) -> str:
    prefix, open_tok, close_tok = style_for(path)
    lines = []
    if open_tok:
        lines.append(open_tok)
    lines.append(f"{prefix}{COPYRIGHT}".rstrip())
    lines.append(prefix.rstrip())
    lines.append(f"{prefix}{LICENSE_ID}".rstrip())
    if close_tok:
        lines.append(close_tok)
    return "\n".join(lines) + "\n"


def insert_header(path: str) -> None:
    with open(path, encoding="utf-8") as fh:
        content = fh.read()
    lines = content.splitlines(keepends=True)
    at = 0
    while at < len(lines) and PRELUDE_RE.match(lines[at]):
        at += 1
    block = header_block(path)
    tail = lines[at:]
    if tail and tail[0].strip():
        block += "\n"
    with open(path, "w", encoding="utf-8") as fh:
        fh.write("".join(lines[:at]) + block + "".join(tail))


def git(*args: str) -> list[str]:
    out = subprocess.run(
        ["git", *args], check=True, capture_output=True, text=True
    ).stdout
    return [line for line in out.splitlines() if line]


def added_since(ref: str) -> list[str]:
    return git("diff", "--name-only", "--diff-filter=A", f"{ref}...HEAD")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    src = ap.add_mutually_exclusive_group(required=True)
    src.add_argument("--added-since", metavar="REF", help="check files added since REF")
    src.add_argument("--all", action="store_true", help="check every tracked file")
    src.add_argument("--files", nargs="+", help="check these paths")
    ap.add_argument(
        "--fix", action="store_true", help="insert the header instead of reporting"
    )
    args = ap.parse_args()

    if args.added_since:
        candidates = added_since(args.added_since)
    elif args.all:
        candidates = git("ls-files")
    else:
        candidates = args.files

    paths = [p for p in candidates if os.path.isfile(p) and is_checkable(p)]
    missing = [p for p in paths if not has_header(p)]

    if not missing:
        print(f"licence headers: {len(paths)} file(s) checked, all compliant")
        return 0

    if args.fix:
        for path in missing:
            insert_header(path)
            print(f"added header: {path}")
        print(f"licence headers: inserted into {len(missing)} file(s)")
        return 0

    print(f"licence headers: {len(missing)} of {len(paths)} checked file(s) missing a header:\n")
    for path in missing:
        print(f"  {path}")
    print(
        "\nEvery source file needs this header (comment syntax per language):\n"
        f"\n  # {COPYRIGHT}\n  #\n  # {LICENSE_ID}\n"
        "\nRun `make license-fix` to insert it, or"
        " `python3 scripts/check_license_headers.py --files <path> --fix`."
    )
    return 1


if __name__ == "__main__":
    sys.exit(main())
