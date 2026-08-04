/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { CopiableText } from "@espressif/dashboard-ui-components/components";
import type { ThingNameCellProps } from "./thing-name-cell.props";

export function ThingNameCell({ thingName, thingId }: ThingNameCellProps) {
  const displayName = thingName?.trim();
  const id = thingId.trim();
  const showIdLine = id.length > 0 && id !== displayName;

  return (
    <div className="min-w-0 flex flex-col">
      {displayName ? (
        <p className="text-sm font-semibold truncate leading-tight">{displayName}</p>
      ) : null}
      {showIdLine ? (
        <CopiableText
          text={id}
          className="text-xs text-muted-foreground truncate leading-tight"
        />
      ) : null}
    </div>
  );
}
