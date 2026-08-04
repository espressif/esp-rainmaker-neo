/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  Typography,
} from "@espressif/dashboard-ui-components/components";
import type { SelectorOptionRowProps } from "./selector-option-row.props";

/**
 * Shared dropdown-option row for the async resource selectors (groups, nodes,
 * firmware, …). It owns the wrapper + name/secondary typography; each selector
 * supplies its own `avatar` so the rows stay visually identical while carrying
 * the right domain icon.
 */
export function SelectorOptionRow({
  avatar,
  label,
  secondaryText,
}: SelectorOptionRowProps) {
  return (
    <div className="flex min-w-0 flex-1 items-center gap-3 rounded-md border border-border bg-muted/40 px-3 py-2.5">
      <div className="shrink-0">{avatar}</div>
      <div className="min-w-0 flex-1">
        <Typography variant="h6" as="div" className="truncate leading-tight">
          {label}
        </Typography>
        {secondaryText ? (
          <Typography
            variant="subtitle2"
            as="div"
            className="mt-0.5 truncate leading-tight"
          >
            {secondaryText}
          </Typography>
        ) : null}
      </div>
    </div>
  );
}
