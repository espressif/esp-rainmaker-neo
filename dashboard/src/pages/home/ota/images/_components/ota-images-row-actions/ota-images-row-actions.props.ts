/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface OtaImagesRowActionsProps {
  /** Full S3 object key (e.g. `ota/led_light`), used for the download URL. */
  imageKey: string;
  /** Display name, used as the download filename. */
  name: string;
}
