/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface QueryRuleRow {
  id: string;
  /** Human-readable rule type label (e.g. "Device Type"). */
  type: string;
  value: string;
}

export interface QueryRulesContentProps {
  rules: QueryRuleRow[];
  onDelete: (index: number) => void;
}
