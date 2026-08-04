/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface NodeGroupStatusBadgeProps {
  /**
   * Raw `DescribeThingGroup` status. Only dynamic groups have one — renders
   * nothing when absent.
   */
  status: string | null | undefined;
}
