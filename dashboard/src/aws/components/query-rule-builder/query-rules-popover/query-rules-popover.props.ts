/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactElement } from "react";

export interface QueryRulesPopoverProps {
  /**
   * Raw AWS IoT fleet-index query string whose rules the popover displays, e.g.
   * `shadow.name.iparams.reported.online:false AND thingName:node*`.
   */
  queryString: string | null | undefined;
  /** Element that opens the popover. Rendered through `PopoverTrigger asChild`. */
  trigger: ReactElement;
  align?: "start" | "center" | "end";
  contentClassName?: string;
}
