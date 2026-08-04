/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface FirmwareAvatarProps {
  /**
   * The image's `fw-type` tag. Undefined until the tagging call resolves, in
   * which case the avatar shows the default firmware glyph.
   */
  fwType?: string;
  size?: number;
}
