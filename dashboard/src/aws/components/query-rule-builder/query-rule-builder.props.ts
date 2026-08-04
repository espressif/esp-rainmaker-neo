/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** A single fleet-index query rule: a field path matched against a value. */
export interface QueryRule {
  field: string;
  value: string;
}

export interface QueryRuleBuilderProps {
  /** Current rules. The component is controlled — it never owns this state. */
  rules: QueryRule[];
  /** Emits the next rules array on add/remove. Mutations flow through here only. */
  onChange: (rules: QueryRule[]) => void;
  /** Card title override. Defaults to `aws:queryRuleBuilder.label`. */
  title?: string;
  /** Card description override. Defaults to `aws:queryRuleBuilder.description`. */
  description?: string;
}
