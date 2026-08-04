/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type {
  IndexFieldRef,
  Junction,
  OperatorId,
  SearchCondition,
} from "./advanced-indices-search.types";
import { OPERATORS_BY_TYPE } from "./operator-config";

interface ParsedQuery {
  conditions: SearchCondition[];
  junctions: Junction[];
}

/**
 * Parses an AWS IoT fleet indexing query string back into structured conditions.
 * Handles basic patterns: field:value, NOT field:value, field:>=value, field:value*, field:*
 * Connected by AND / OR junctions.
 *
 * Complex nested queries or grouped expressions are not fully supported and will
 * be treated as best-effort parsing.
 */
export function parseQueryString(
  query: string,
  fields: readonly IndexFieldRef[],
): ParsedQuery {
  const result: ParsedQuery = { conditions: [], junctions: [] };

  if (!query.trim()) {return result;}

  const fieldMap = new Map(fields.map((f) => [f.name, f]));

  // Split by AND/OR while capturing the junction
  const parts = query.split(/\s+(AND|OR)\s+/);

  for (let i = 0; i < parts.length; i++) {
    const part = parts[i].trim();

    // Odd indices are junctions
    if (i % 2 === 1) {
      if (part === "AND" || part === "OR") {
        result.junctions.push(part);
      }
      continue;
    }

    const condition = parseFragment(part, fieldMap);
    if (condition) {
      result.conditions.push(condition);
    }
  }

  return result;
}

function parseFragment(
  fragment: string,
  fieldMap: Map<string, IndexFieldRef>,
): SearchCondition | null {
  let isNegated = false;
  let working = fragment.trim();

  if (working.startsWith("NOT ")) {
    isNegated = true;
    working = working.slice(4).trim();
  }

  // Match field:operator_value pattern
  const colonIdx = working.indexOf(":");
  if (colonIdx === -1) {return null;}

  const field = working.slice(0, colonIdx);
  let rawValue = working.slice(colonIdx + 1);

  const fieldInfo = fieldMap.get(field);
  const fieldType = fieldInfo?.type ?? "String";

  // Determine operator and extract clean value
  let operator: OperatorId;

  if (rawValue === "*") {
    operator = "exists";
    rawValue = "";
  } else if (isNegated) {
    operator = "neq";
  } else if (rawValue.startsWith(">=")) {
    operator = "gte";
    rawValue = rawValue.slice(2);
  } else if (rawValue.startsWith(">")) {
    operator = "gt";
    rawValue = rawValue.slice(1);
  } else if (rawValue.startsWith("<=")) {
    operator = "lte";
    rawValue = rawValue.slice(2);
  } else if (rawValue.startsWith("<")) {
    operator = "lt";
    rawValue = rawValue.slice(1);
  } else if (rawValue.endsWith("*")) {
    operator = "starts_with";
    rawValue = rawValue.slice(0, -1);
  } else {
    operator = "eq";
  }

  // Validate the operator is valid for the field type
  const validOperators = OPERATORS_BY_TYPE[fieldType];
  if (!validOperators.some((op) => op.id === operator)) {
    operator = "eq";
  }

  return {
    field,
    fieldType,
    operator,
    value: rawValue,
  };
}
