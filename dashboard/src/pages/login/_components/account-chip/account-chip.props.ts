/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface AccountChipProps {
  /** The address the flow is signing in, shown under the avatar. */
  email: string;
  /**
   * Makes the card interactive. Screen 1 passes its Continue action so clicking
   * the account itself signs in with it; the password screen leaves the chip
   * inert — there the address is context, not a choice.
   */
  onClick?: () => void;
}
