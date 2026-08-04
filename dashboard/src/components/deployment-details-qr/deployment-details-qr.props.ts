/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface DeploymentDetailsQrProps {
  /** URL encoded into the QR code. */
  url: string;
  /** Called when the QR PNG data URL is ready (e.g. for external download flows). */
  onReady?: (dataUrl: string) => void;
}
