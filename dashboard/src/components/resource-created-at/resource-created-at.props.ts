/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface ResourceCreatedAtProps {
  /** Creation timestamp in milliseconds. Renders nothing when absent. */
  ts: number | null | undefined;
}
