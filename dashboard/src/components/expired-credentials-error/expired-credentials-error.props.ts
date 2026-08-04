/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export type ExpiredCredentialsErrorProps = {
  /** Escape hatch out of the dead session — clears tokens and returns to `/login`. */
  onBackToLogin: () => void;
};
