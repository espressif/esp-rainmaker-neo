/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface OtaJobGroupFilterProps {
  /** Currently applied group name, or undefined when no group filter is active. */
  value?: string;
  /** Commit a new group selection (or clear it) to the parent filter state. */
  onChange: (groupName: string | undefined) => void;
}
