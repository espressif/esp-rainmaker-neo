/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface PushIntegrationFormSheetProps {
  /** Close the sheet. The parent owns mount/unmount of this wrapper. */
  onClose: () => void;
}
