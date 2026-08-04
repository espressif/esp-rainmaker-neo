/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface ChangePasswordFormProps {
  /** Called once Cognito has accepted the new password. */
  onSuccess: () => void;
}
