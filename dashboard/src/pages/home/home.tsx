/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo, useEffect, useCallback } from "react";
import { Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { VerificationInProgress } from "@espressif/dashboard-ui-components/components";
import {
  WorkspaceLayout,
  isSidebarGroup,
} from "@espressif/dashboard-ui-components/layouts";
import { appConfig } from "@/lib/app-config";
import { routesConfig } from "@/config/app-routes.config";
import {
  ACCOUNT_SETTINGS_BASE_PATH,
  ACCOUNT_SETTINGS_TABS_BY_ID,
} from "@/config/account-settings.config";
import { LANGUAGE_OPTIONS } from "@/config/language.config";
import { getSidebarConfig } from "@/config/sidebar/sidebar.config";
import { useAppStore } from "@/stores/app.store";
import { useAuthStore } from "@/stores/auth.store";
import { useUserStore } from "@/stores/user.store";
import { useGetUserCreds, toAwsCredentials } from "@/api";
import { logout, loginHrefWithRedirect } from "@/lib/auth";
import { useSessionKeeper } from "@/hooks/use-session-keeper";
import { buildBreadcrumbs } from "@/lib/breadcrumbs/build-breadcrumbs";
import { assertSidebarGroupRedirects } from "@/lib/navigation/assert-sidebar-group-redirects";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import { ExpiredCredentialsError } from "@/components/expired-credentials-error";
import { HeaderRightContent } from "@/components/header-right-content";
import { SidebarAccountCard } from "@/components/sidebar-account-card";
import { presetFallbackLogoSlots } from "@/components/brand-logo";

const PASSWORD_TAB = ACCOUNT_SETTINGS_TABS_BY_ID.password;

export default function Home() {
  const { t, i18n } = useTranslation("common");
  const location = useLocation();
  const navigate = useNavigate();
  const {
    sidebarCollapsed,
    setSidebarCollapsed,
    darkMode,
    setDarkMode,
    language,
    setLanguage,
  } = useAppStore();
  const loggedInUserName = useUserStore((s) => s.loggedInUserName);
  const { credentials, setCredentials, isCredentialsValid } = useAuthStore();

  const needsCreds = !credentials || !isCredentialsValid();
  const credsQuery = useGetUserCreds({ enabled: needsCreds });

  useEffect(() => {
    if (credsQuery.data) {
      setCredentials(toAwsCredentials(credsQuery.data));
    }
  }, [credsQuery.data, setCredentials]);

  // Renews the Cognito tokens and the credentials derived from them a couple of minutes
  // before they lapse, for users who asked to stay signed in.
  useSessionKeeper();

  const goToAccountSettings = useCallback(() => {
    void navigate({ to: ACCOUNT_SETTINGS_BASE_PATH });
  }, [navigate]);

  const goToChangePassword = useCallback(() => {
    void navigate({ to: PASSWORD_TAB.path });
  }, [navigate]);

  /**
   * Label, icon and destination all come from the tab config, so the header shortcut
   * can never drift from the account settings nav it jumps to.
   */
  const accountMenuItems = useMemo(
    () => [
      {
        id: "actions",
        items: [
          {
            id: "change-password",
            label: t(`account-settings:${PASSWORD_TAB.labelKey}`, PASSWORD_TAB.fallback),
            startIcon: <PASSWORD_TAB.icon className="h-4 w-4" />,
            onClick: goToChangePassword,
          },
        ],
      },
    ],
    [t, goToChangePassword],
  );

  const sidebarEntries = useMemo(() => getSidebarConfig(t), [t]);

  const breadcrumbs = useMemo(
    () => buildBreadcrumbs(routesConfig, location.pathname, t),
    [location.pathname, t],
  );

  if (import.meta.env.DEV) {
    assertSidebarGroupRedirects(
      sidebarEntries.filter(isSidebarGroup),
      routesConfig,
    );
  }

  if (needsCreds && credsQuery.isLoading) {
    return (
      <div className="flex min-h-screen w-full items-center justify-center">
        <VerificationInProgress title={t("verifyingCredentials", "Verifying credentials...")} />
      </div>
    );
  }

  if (credsQuery.error) {
    return (
      <ExpiredCredentialsError
        onBackToLogin={() => logout(loginHrefWithRedirect(location.href))}
      />
    );
  }

  return (
    <WorkspaceLayout
      appName={appConfig.projectName}
      // Empty while `projectName` names a preset; a fork omits it and these slots
      // render the configured assets instead.
      {...presetFallbackLogoSlots(darkMode)}
      sidebarConfig={{
        items: sidebarEntries,
        currentPath: location.pathname.replace(/\$/g, "%24"),
        indexPath: "/home",
        LinkComponent: TanstackRouterLink,
        allowCollapsible: true,
        footer: <SidebarAccountCard />,
        hideCompanyBranding: true,
      }}
      headerConfig={{
        userName: loggedInUserName || "",
        userEmail: loggedInUserName || "",
        breadcrumbs,
        rightContent: <HeaderRightContent />,
      }}
      footerConfig={{
        hidden: appConfig.hideFooter,
      }}
      sidebarOpen={!sidebarCollapsed}
      defaultSidebarOpen={true}
      onSidebarOpenChange={(open) => setSidebarCollapsed(!open)}
      onLogoutClick={() => void logout()}
      onProfileCardClick={goToAccountSettings}
      onColorThemeChange={(theme) => setDarkMode(theme === "dark")}
      colorTheme={darkMode ? "dark" : "light"}
      languages={LANGUAGE_OPTIONS.map((option) => ({
        code: option.code,
        label: t(option.labelKey, option.fallback),
      }))}
      currentLanguage={language}
      onLanguageChange={(code) => {
        setLanguage(code as typeof language);
        void i18n.changeLanguage(code);
      }}
      accountMenuItems={accountMenuItems}
    >
      <Outlet />
    </WorkspaceLayout>
  );
}
