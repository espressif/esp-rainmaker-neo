/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { FieldType, Operator, OperatorId } from "./advanced-indices-search.types";

/**
 * `label` is the badge token — a symbol for most operators, so only the two
 * word-shaped ones carry a `labelKey`. `description` is the English fallback for
 * `descriptionKey`; both live under `common` because the search bar and the query
 * rule builder share this table across routes.
 */
export const OPERATORS_BY_TYPE: Record<FieldType, Operator[]> = {
  String: [
    { id: "eq", label: "=", descriptionKey: "common:searchOperators.eq", description: "equals" },
    { id: "neq", label: "!=", descriptionKey: "common:searchOperators.neq", description: "not equals" },
    {
      id: "starts_with",
      label: "starts with",
      labelKey: "common:searchOperators.labels.startsWith",
      descriptionKey: "common:searchOperators.startsWith",
      description: "starts with",
    },
    {
      id: "exists",
      label: "exists",
      labelKey: "common:searchOperators.labels.exists",
      descriptionKey: "common:searchOperators.exists",
      description: "field exists",
      noValue: true,
    },
  ],
  Number: [
    { id: "eq", label: "=", descriptionKey: "common:searchOperators.eq", description: "equals" },
    { id: "neq", label: "!=", descriptionKey: "common:searchOperators.neq", description: "not equals" },
    { id: "gt", label: ">", descriptionKey: "common:searchOperators.gt", description: "greater than" },
    { id: "gte", label: ">=", descriptionKey: "common:searchOperators.gte", description: "greater or equal" },
    { id: "lt", label: "<", descriptionKey: "common:searchOperators.lt", description: "less than" },
    { id: "lte", label: "<=", descriptionKey: "common:searchOperators.lte", description: "less or equal" },
    {
      id: "exists",
      label: "exists",
      labelKey: "common:searchOperators.labels.exists",
      descriptionKey: "common:searchOperators.exists",
      description: "field exists",
      noValue: true,
    },
  ],
  Boolean: [
    { id: "eq", label: "=", descriptionKey: "common:searchOperators.eq", description: "equals" },
    { id: "neq", label: "!=", descriptionKey: "common:searchOperators.neq", description: "not equals" },
  ],
};

/**
 * Build an AWS IoT fleet indexing query fragment for a single condition.
 */
export function buildFragment(
  field: string,
  operator: OperatorId,
  value: string,
): string {
  switch (operator) {
    case "eq":
      return `${field}:${value}`;
    case "neq":
      return `NOT ${field}:${value}`;
    case "starts_with":
      return `${field}:${value}*`;
    case "gt":
      return `${field}:>${value}`;
    case "gte":
      return `${field}:>=${value}`;
    case "lt":
      return `${field}:<${value}`;
    case "lte":
      return `${field}:<=${value}`;
    case "exists":
      return `${field}:*`;
    default:
      return `${field}:${value}`;
  }
}
