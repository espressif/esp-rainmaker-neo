/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface DynamicMembershipNoticeProps {
  /** Query string defining the group's membership, surfaced through the "View rules" popover. */
  queryString: string | null | undefined;
}
