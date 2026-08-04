/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface OtaImageNameCellProps {
  name: string;
  /** Object size in bytes, shown as a badge next to the name. */
  size: number;
  /** S3 ETag, shown as the MD5 sub-line. Absent until the List call resolves. */
  md5?: string;
  /** `fw-type` tag, drives the avatar glyph once tags load. */
  fwType?: string;
}
