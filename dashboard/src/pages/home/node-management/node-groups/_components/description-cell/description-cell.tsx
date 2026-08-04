/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Tooltip } from "@espressif/dashboard-ui-components";
import type { DescriptionCellProps } from "./description-cell.props";

const TRUNCATE_LIMIT = 100;

export function DescriptionCell({ description }: DescriptionCellProps) {
  if (!description) {
    return null;
  }

  const shouldTruncate = description.length > TRUNCATE_LIMIT;
  const displayText = shouldTruncate
    ? `${description.slice(0, TRUNCATE_LIMIT)}…`
    : description;

  const content = (
    <span className="text-sm text-muted-foreground">{displayText}</span>
  );

  if (!shouldTruncate) {
    return content;
  }

  return (
    <Tooltip content={description}>
      <span className="inline-flex max-w-full">{content}</span>
    </Tooltip>
  );
}
