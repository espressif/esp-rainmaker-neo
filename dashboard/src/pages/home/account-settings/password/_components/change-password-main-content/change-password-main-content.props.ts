/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { SetPasswordMode } from "../../_utils/password-factor";

export interface ChangePasswordMainContentProps {
  /** Whether Cognito has already accepted a new password in this visit. */
  isPasswordChanged: boolean;
  /** Raised by the form once the change succeeds. */
  onPasswordChanged: () => void;
  /** Whether this admin changed an existing password or set their first one. */
  mode: SetPasswordMode;
}
