/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { AlexaConfigGetResponse } from "@/api/integrations";

export interface AlexaMainContentProps {
  /** Fetched Alexa configuration; `undefined` while loading or when absent. */
  data: AlexaConfigGetResponse | undefined;
  isLoading: boolean;
  error: Error | null;
  /** Opens the configuration sheet from both the empty state and the saved card. */
  onConfigure: () => void;
}
