/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface QueryRulesListProps {
  /**
   * Raw AWS IoT fleet-index query string, e.g.
   * `shadow.name.iparams.reported.online:false AND thingName:node*`.
   */
  queryString: string | null | undefined;
  className?: string;
}
