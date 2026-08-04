/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@espressif/dashboard-ui-components/components";
import {
  ACCOUNT_SETTINGS_TABS,
  type AccountSettingsTabId,
} from "@/config/account-settings.config";
import type { AccountSettingsNavSelectProps } from "./account-settings-nav-select.props";

/**
 * Small-screen replacement for the nav rail. A listbox cannot be a set of links, so
 * unlike the rail this navigates on change — open-in-new-tab is a desktop-only affordance.
 */
export default function AccountSettingsNavSelect({
  activeTabId,
  onSelectTab,
}: AccountSettingsNavSelectProps) {
  const { t } = useTranslation("account-settings");

  const handleValueChange = useCallback(
    (value: string) => {
      onSelectTab(value as AccountSettingsTabId);
    },
    [onSelectTab],
  );

  return (
    <Select value={activeTabId ?? undefined} onValueChange={handleValueChange}>
      <SelectTrigger
        className="w-full"
        aria-label={t("navAriaLabel", "Account settings sections")}
      >
        <SelectValue
          placeholder={t("navSelect.placeholder", "Select a section")}
        />
      </SelectTrigger>
      <SelectContent>
        {ACCOUNT_SETTINGS_TABS.map((tab) => {
          const Icon = tab.icon;
          return (
            <SelectItem key={tab.id} value={tab.id}>
              <span className="flex items-center gap-2">
                <Icon className="h-4 w-4 shrink-0" />
                {t(tab.labelKey, tab.fallback)}
              </span>
            </SelectItem>
          );
        })}
      </SelectContent>
    </Select>
  );
}
