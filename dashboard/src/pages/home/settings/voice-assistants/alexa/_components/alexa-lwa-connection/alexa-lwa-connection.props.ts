/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface AlexaLwaConnectionProps {
  /** Security profile client id of the connected Amazon account, or undefined when none is connected. */
  connectedClientId?: string;
}
