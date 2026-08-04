/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface QueryRuleValueFieldProps {
  /** Selected rule field name; drives how values are sourced. */
  fieldName: string;
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}
