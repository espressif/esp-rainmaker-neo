/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { QueryRule } from "./query-rule-builder.props";

/**
 * Serialises rules into an AWS IoT fleet-indexing query string
 * (`field:value AND field:value`). Blank pairs are dropped.
 */
export function buildQueryFromRules(rules: QueryRule[]): string {
  return rules
    .filter((rule) => rule.field && rule.value)
    .map((rule) => `${rule.field}:${rule.value}`)
    .join(" AND ");
}
