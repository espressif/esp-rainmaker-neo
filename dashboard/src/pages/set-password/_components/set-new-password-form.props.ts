/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface SetNewPasswordFormProps {
  /** Address the confirmation code was mailed to. */
  email: string;
  /**
   * Whether a code was just mailed on the way here. Arms the resend cooldown on
   * arrival; admins who already hold a code get a live Resend immediately.
   */
  codeJustSent: boolean;
  /** Fired once the password has been changed. */
  onSuccess: () => void;
  /** Fired after a fresh code has been mailed, so the page can reflect it. */
  onCodeResent: () => void;
}
