/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { PublishedBrand } from "@/utils/rainmaker-brand/rainmaker-brand";

export interface BrandingMainContentProps {
  /** The brand phones get today; null when the deployment has none. */
  brand: PublishedBrand | null | undefined;
  isLoading: boolean;
  error: Error | null;
}
