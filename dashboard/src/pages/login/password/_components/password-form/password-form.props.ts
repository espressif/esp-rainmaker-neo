/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface PasswordFormProps {
  allowKeepMeSignedIn: boolean;
  keepSignedIn: boolean;
  onKeepSignedInChange: (checked: boolean) => void;
  isSubmitting: boolean;
  /** Loading state of the auto-requested reset code behind "Forgot password?". */
  isRequestingReset: boolean;
  onSubmit: (password: string) => void;
  onForgotPassword: () => void;
}
