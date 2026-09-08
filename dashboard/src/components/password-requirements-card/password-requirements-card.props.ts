/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { RequirementListItem } from "@espressif/dashboard-ui-components/components";

export interface PasswordRequirementsCardProps {
  /**
   * Policy rules with their current pass/fail state, in display order. Callers
   * derive these from `evaluatePasswordPolicy` so the same source of truth drives
   * the checklist and the validating schema.
   */
  items: RequirementListItem[];
  /** Optional extra classes; the default `mt-4` matches how the card sits under an input. */
  className?: string;
}
