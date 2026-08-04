/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TypeModelCellProps } from "./type-model-cell.props";

export function TypeModelCell({ type, model }: TypeModelCellProps) {
  const typeStr = type?.trim();
  const modelStr = model?.trim();

  if (!typeStr && !modelStr) {
    return null;
  }

  const primary = typeStr || modelStr;
  const secondary = typeStr && modelStr ? modelStr : undefined;

  return (
    <div className="min-w-0 flex flex-col">
      <p className="text-sm font-semibold truncate leading-tight">{primary}</p>
      {secondary ? (
        <p className="text-xs text-muted-foreground truncate leading-tight">
          {secondary}
        </p>
      ) : null}
    </div>
  );
}
