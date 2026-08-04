/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { QueryRule } from "../../query-rule-builder.props";

export interface QueryRulePopoverProps {
  /** Called with a validated rule when the user submits the popover form. */
  onAdd: (rule: QueryRule) => void;
}
