/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { AlexaConfigGetResponse } from "@/api/integrations";

export interface AlexaConfigurationCardProps {
  /** Saved Alexa configuration returned by the GET endpoint. */
  config: AlexaConfigGetResponse;
  /** Opens the edit sheet seeded with the saved configuration. */
  onEdit: () => void;
}
