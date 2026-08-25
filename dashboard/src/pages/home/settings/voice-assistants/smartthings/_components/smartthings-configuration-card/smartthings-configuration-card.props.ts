/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { SmartThingsConfigGetResponse } from "@/api/integrations";

export interface SmartThingsConfigurationCardProps {
  /** Saved SmartThings configuration returned by the GET endpoint. */
  config: SmartThingsConfigGetResponse;
  /** Opens the edit sheet for the saved configuration. */
  onEdit: () => void;
}
