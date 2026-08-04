/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { VoiceAssistantTab } from "../voice-assistants-page-tabs";

export interface ConfigurationValuesProps {
  /** Card header label, e.g. "Configuration values". */
  title: string;
  /** Open assistant tab; selects which assistant-specific value is listed. */
  activeTab: VoiceAssistantTab;
}
