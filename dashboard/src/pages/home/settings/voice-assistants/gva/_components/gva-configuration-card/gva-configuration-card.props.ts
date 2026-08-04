/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { GvaConfigGetResponse } from "@/api/integrations";

export interface GvaConfigurationCardProps {
  /** Saved GVA service-account configuration returned by the GET endpoint. */
  config: GvaConfigGetResponse;
  /** Opens the edit sheet for the saved configuration. */
  onEdit: () => void;
}
