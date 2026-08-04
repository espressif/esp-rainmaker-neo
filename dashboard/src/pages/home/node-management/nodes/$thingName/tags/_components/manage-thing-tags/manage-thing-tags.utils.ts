/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface TagRow {
  key: string;
  value: string;
}

export function tagRecordToRows(
  record: Record<string, string> | undefined,
): TagRow[] {
  return Object.entries(record ?? {})
    .map(([key, value]) => ({ key, value }))
    .sort((a, b) =>
      a.key.localeCompare(b.key, undefined, { sensitivity: "base" }),
    );
}
