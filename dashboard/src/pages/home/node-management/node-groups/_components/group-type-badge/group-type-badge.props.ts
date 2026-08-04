/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface GroupTypeBadgeProps {
  /**
   * The group's fleet-index query string. Present for dynamic groups, `null`
   * for static ones — which is what decides the badge rendered.
   */
  queryString: string | null;
}
