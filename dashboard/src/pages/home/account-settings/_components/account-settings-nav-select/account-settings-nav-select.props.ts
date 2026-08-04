/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { AccountSettingsTabId } from "@/config/account-settings.config";

export interface AccountSettingsNavSelectProps {
  /** Selected tab, or `null` when the pathname matches no known tab. */
  activeTabId: AccountSettingsTabId | null;
  onSelectTab: (tabId: AccountSettingsTabId) => void;
}
