#!/bin/bash
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# Usage go_deps.sh abc_main.go build/abc/bootstrap
#
# Emits a make fragment listing every in-tree Go source a lambda is built from,
# so editing a shared package rebuilds the lambdas that import it.
#
# Packages are selected by resolving each dependency's source directory and
# keeping the ones inside this repository, not by matching the module path — the
# previous `grep rmng` filter matched nothing once the module was renamed, and
# resolving directories also covers the go.work submodules.

set -eu

MAIN_GO=$1
TARGET=$2
DEPS_FILE=$TARGET.deps
TMP_FILE=$DEPS_FILE.tmp

mkdir -p "$(dirname "$TARGET")"
PKG_DIR=$(dirname "$MAIN_GO")
REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)

# A bare relative path is an import path to `go list`, not a directory, so it
# would resolve against the standard library and find nothing.
case "$PKG_DIR" in
	/* | ./* | ../*) ;;
	*) PKG_DIR=./$PKG_DIR ;;
esac

# Match the lambda build's platform and tags — the dependency graph differs by
# GOOS/GOARCH.
export GOOS=linux GOARCH=arm64 CGO_ENABLED=0

# Let a failure here stop the build. Writing a dependency file that tracks
# nothing is how stale binaries shipped in the first place.
# .Dir is empty for standard-library packages, so they drop out on their own.
DEP_DIRS=$(go list -deps -tags "lambda.norpc" -f '{{.Dir}}' "$PKG_DIR")

{
    printf '%s: %s %s' "$TARGET" "$MAIN_GO" "$DEPS_FILE"
    for dir in $DEP_DIRS; do
        case "$dir" in
            "$REPO_ROOT"/*) ;;
            *) continue ;;   # module cache or stdlib — not ours to track
        esac
        for f in "$dir"/*.go; do
            case "$f" in
                *_test.go) continue ;;
                *'*.go') continue ;;   # glob stayed literal: no files
            esac
            printf ' %s' "$f"
        done
    done
    printf '\n'
} > "$TMP_FILE"

mv "$TMP_FILE" "$DEPS_FILE"
