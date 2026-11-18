/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** Which way the skill is set up. Manual is first: it is the original flow. */
export type AlexaSetupTab = "manual" | "automatic";

export interface AlexaSetupTabsProps {
  activeTab: AlexaSetupTab;
  onTabChange: (tab: AlexaSetupTab) => void;
}
