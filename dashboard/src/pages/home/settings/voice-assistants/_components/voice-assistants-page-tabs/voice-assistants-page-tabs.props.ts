/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export type VoiceAssistantTab = "alexa" | "gva";

export interface VoiceAssistantsPageTabsProps {
  activeTab: VoiceAssistantTab;
  onTabChange: (value: VoiceAssistantTab) => void;
}
