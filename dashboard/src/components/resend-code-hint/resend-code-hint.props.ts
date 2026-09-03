/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface ResendCodeHintProps {
  /** While true, the countdown label renders instead of the live control. */
  isCoolingDown: boolean;
  /**
   * Translated countdown text with the seconds already interpolated. A string
   * (not a key) because each caller owns its i18n namespace — the login flow
   * and the reset flow deliberately keep separate ones.
   */
  countdownLabel: string;
  /** Translated label of the live resend control. */
  resendLabel: string;
  isResending: boolean;
  onResend: () => void;
}
