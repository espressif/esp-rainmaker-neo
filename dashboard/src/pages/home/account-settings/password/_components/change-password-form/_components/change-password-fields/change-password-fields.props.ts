/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { RequirementListItem } from "@espressif/dashboard-ui-components/components";
import type { SetPasswordMode } from "../../../../_utils/password-factor";

export interface ChangePasswordFieldsProps {
  /** Whether this admin is changing an existing password or setting a first one. */
  mode: SetPasswordMode;
  /** Policy rules with their current pass/fail state, shown under the new password. */
  requirementItems: RequirementListItem[];
}
