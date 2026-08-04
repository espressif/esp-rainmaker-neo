/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * True only for same-origin relative paths.
 *
 * Rejects protocol-relative (`//evil.com`) and absolute URLs, so a hand-edited
 * `?redirect=` cannot turn a post-login navigation into an open redirect.
 */
export function isInternalPath(path: unknown): path is string {
  return (
    typeof path === "string" &&
    path.startsWith("/") &&
    !path.startsWith("//")
  );
}
