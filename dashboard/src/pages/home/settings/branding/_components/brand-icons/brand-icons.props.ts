/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { CSSProperties } from "react";
import type { BrandIconGroup } from "../../branding.utils";

export interface BrandIconsProps {
  groups: BrandIconGroup[];
  /** The brand's own background, so icons read the way the phone shows them. */
  iconStyle: CSSProperties;
}
