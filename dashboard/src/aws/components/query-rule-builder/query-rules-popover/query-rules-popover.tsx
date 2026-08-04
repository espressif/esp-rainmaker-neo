/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useState } from "react";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@espressif/dashboard-ui-components/components";
import { cn } from "@/utils/utils";
import { QueryRulesList } from "../query-rules-list";
import type { QueryRulesPopoverProps } from "./query-rules-popover.props";

/**
 * Reveals a dynamic group's membership rules in a popover. The trigger is supplied by the caller so
 * the same disclosure works from a badge in a table row or a link button inside a notice.
 */
export default function QueryRulesPopover({
  queryString,
  trigger,
  align = "start",
  contentClassName,
}: QueryRulesPopoverProps) {
  const [open, setOpen] = useState(false);

  // Triggers can sit inside a clickable table row; keep popover interaction from navigating.
  const stopPropagation = useCallback(
    (event: React.SyntheticEvent) => event.stopPropagation(),
    [],
  );

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{trigger}</PopoverTrigger>
      <PopoverContent
        align={align}
        className={cn("w-80", contentClassName)}
        onClick={stopPropagation}
      >
        <QueryRulesList queryString={queryString} />
      </PopoverContent>
    </Popover>
  );
}
