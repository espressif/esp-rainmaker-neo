/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface OtpFormProps {
  allowKeepMeSignedIn: boolean;
  keepSignedIn: boolean;
  onKeepSignedInChange: (checked: boolean) => void;
  isSubmitting: boolean;
  isResending: boolean;
  /** Seconds until Resend re-arms; `0` renders the live Resend control. */
  resendSecondsLeft: number;
  onSubmit: (code: string) => void;
  onResend: () => void;
}
