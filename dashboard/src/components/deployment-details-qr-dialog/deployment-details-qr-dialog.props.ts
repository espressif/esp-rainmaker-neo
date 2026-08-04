/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface DeploymentDetailsQrDialogProps {
  /** URL encoded into the QR code. */
  url: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}
