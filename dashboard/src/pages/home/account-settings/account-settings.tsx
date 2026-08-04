/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { PageContainer } from "@espressif/dashboard-ui-components/components";
import {
  ACCOUNT_SETTINGS_TABS_BY_ID,
  getActiveAccountSettingsTabId,
  type AccountSettingsTabId,
} from "@/config/account-settings.config";
import { AccountSettingsNavCard } from "./_components/account-settings-nav-card";
import { AccountSettingsNavSelect } from "./_components/account-settings-nav-select";

/**
 * Route-backed shell for the account settings tabs: nav on the left, the active tab's
 * page through <Outlet/> on the right. Tab bodies are their own routes, so the active
 * tab is derived from the URL rather than component state.
 *
 * The nav rail is links-only (see `AccountSettingsNavCard`), so only the small-screen
 * `Select` needs a navigation handler.
 */
export default function AccountSettings() {
  const { t } = useTranslation("account-settings");
  const location = useLocation();
  const navigate = useNavigate();

  const activeTabId = useMemo(
    () => getActiveAccountSettingsTabId(location.pathname),
    [location.pathname],
  );

  const handleSelectTab = useCallback(
    (tabId: AccountSettingsTabId) => {
      void navigate({ to: ACCOUNT_SETTINGS_TABS_BY_ID[tabId].path });
    },
    [navigate],
  );

  return (
    <PageContainer
      noGutters
      maxWidth="xl"
      heading={t("pageTitle", "Account Settings")}
    >
      <div className="grid grid-cols-12 items-start gap-6">
        <div className="col-span-12 lg:hidden">
          <AccountSettingsNavSelect
            activeTabId={activeTabId}
            onSelectTab={handleSelectTab}
          />
        </div>

        {/*
          Rail and Select are swapped with CSS rather than a JS breakpoint hook: the
          library's `useIsMobile` is fixed at 768px and would disagree with `lg`.
        */}
        <div className="hidden lg:sticky lg:top-6 lg:col-span-4 lg:block lg:self-start">
          <AccountSettingsNavCard activeTabId={activeTabId} />
        </div>

        <div className="col-span-12 min-w-0 lg:col-span-8">
          <Outlet />
        </div>
      </div>
    </PageContainer>
  );
}
