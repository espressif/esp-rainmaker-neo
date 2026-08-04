/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Derive a friendly file name from a `cert_file_s3_path`. The API returns paths
 * like `s3://<bucket>/system/node_certs/<8char>_<original>.csv`; we take
 * everything after the last `/` and drop the leading `<hash>_` prefix.
 */
export function getRegistrationJobFileName(
  s3Path: string | null | undefined,
): string | undefined {
  if (!s3Path) {
    return undefined;
  }
  const last = s3Path.split("/").pop() ?? "";
  if (!last) {
    return undefined;
  }
  const underscoreIdx = last.indexOf("_");
  return underscoreIdx >= 0 ? last.slice(underscoreIdx + 1) : last;
}
