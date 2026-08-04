/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { Junction, SearchCondition } from "./advanced-indices-search.types";
import { buildFragment } from "./operator-config";

/**
 * Assembles a full AWS IoT fleet indexing query string from structured conditions.
 * Each pair of adjacent conditions is joined by the corresponding junction (AND/OR).
 *
 * Example output: "thingName:myThing AND NOT thingTypeName:legacy"
 */
export function buildQueryString(
  conditions: SearchCondition[],
  junctions: Junction[],
): string {
  if (conditions.length === 0) {return "";}

  return conditions
    .map((condition, i) => {
      const fragment = buildFragment(
        condition.field,
        condition.operator,
        condition.value,
      );
      if (i === 0) {return fragment;}
      const junction = junctions[i - 1] ?? "AND";
      return `${junction} ${fragment}`;
    })
    .join(" ");
}
