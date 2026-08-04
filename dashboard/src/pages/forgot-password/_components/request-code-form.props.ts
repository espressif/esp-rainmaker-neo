/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface RequestCodeFormProps {
  /** Fired once the confirmation code has been mailed. */
  onCodeSent: (email: string) => void;
  /**
   * Fired when the admin already holds a code. No request is made, which
   * leaves the ~5 reset requests Cognito allows per user per hour untouched.
   */
  onHasCode: (email: string) => void;
}
