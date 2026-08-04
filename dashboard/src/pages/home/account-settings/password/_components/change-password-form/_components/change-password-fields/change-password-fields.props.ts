/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { RequirementListItem } from "@espressif/dashboard-ui-components/components";

export interface ChangePasswordFieldsProps {
  /** Policy rules with their current pass/fail state, shown under the new password. */
  requirementItems: RequirementListItem[];
}
