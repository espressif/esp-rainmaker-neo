/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { CopiableText } from "@espressif/dashboard-ui-components/components";
import type { ThingNameCellProps } from "./thing-name-cell.props";

export function ThingNameCell({ nodeId, displayName }: ThingNameCellProps) {
  const name = displayName?.trim();
  const id = nodeId.trim();

  if (!name) {

    return (
      <div className="min-w-0 flex flex-col">
        <CopiableText
          text={id}
          className="text-sm font-normal truncate leading-tight"
        />
      </div>
    );
  }

  return (
    <div className="min-w-0 flex flex-col">
      <p className="text-sm font-semibold truncate leading-tight">{name}</p>
      <CopiableText
        text={id}
        className="text-xs text-muted-foreground truncate leading-tight"
      />
    </div>
  );
}
