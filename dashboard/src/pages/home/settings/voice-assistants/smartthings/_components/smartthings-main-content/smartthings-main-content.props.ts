/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { SmartThingsConfigGetResponse } from "@/api/integrations";

export interface SmartThingsMainContentProps {
  /** Fetched SmartThings configuration; `undefined` while loading or when absent. */
  data: SmartThingsConfigGetResponse | undefined;
  isLoading: boolean;
  error: Error | null;
  /** Opens the configure/edit sheet (empty-state Configure and card Edit). */
  onConfigure: () => void;
}
