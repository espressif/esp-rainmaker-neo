/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface BackToSignInLinkProps {
  /**
   * Back target. The sign-in step pages pass their back-target-rule result
   * (`/login`, `/login/email`, or `/login/otp`); everyone else takes the
   * default entry route.
   */
  to?: string;
}
