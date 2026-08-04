/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface ChangePasswordMainContentProps {
  /** Whether Cognito has already accepted a new password in this visit. */
  isPasswordChanged: boolean;
  /** Raised by the form once the change succeeds. */
  onPasswordChanged: () => void;
}
