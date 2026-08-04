/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { QueryRule } from "../../query-rule-builder.props";

export interface QueryRuleFormProps {
  /** Called with a validated rule when the user submits. */
  onSubmit: (rule: QueryRule) => void;
  /** Called when the user clears the form (container may close itself). */
  onClear?: () => void;
}
