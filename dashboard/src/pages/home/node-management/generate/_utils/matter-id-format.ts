/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** Format a 16-bit id as 0x-prefixed, 4-digit uppercase hex (e.g. 0xFFF2). */
export function hex4(value: number): string {
  return `0x${(value & 0xffff).toString(16).toUpperCase().padStart(4, "0")}`;
}
