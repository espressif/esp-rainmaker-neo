/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface UserProfileCardProps {
  /** Primary identity line — claims email, falling back to the login username. */
  email: string;
  /** Secondary line — cognito username; hidden when null or equal to email. */
  username: string | null;
  className?: string;
}
