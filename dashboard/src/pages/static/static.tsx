/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { Outlet, useLocation } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { WorkspaceLayout } from "@espressif/dashboard-ui-components/layouts";
import { appConfig } from "@/lib/app-config";
import { routesConfig, STATIC_BASE_PATH } from "@/config/app-routes.config";
import { LANGUAGE_OPTIONS } from "@/config/language.config";
import { useAppStore } from "@/stores/app.store";
import { buildBreadcrumbs } from "@/lib/breadcrumbs/build-breadcrumbs";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import { configuredLogoSlots } from "@/components/brand-logo";
import { getStaticSidebarConfig } from "./static-sidebar.config";

/**
 * Shell for the public static documents.
 *
 * Deliberately user-agnostic: no credentials, no user store, no profile card and no logout.
 * The account menu is reduced to language and theme — passing at least one of those is what
 * keeps it rendered at all, and without it a visitor on a locale-aware document would have
 * no way to switch locale.
 */
export default function Static() {
  const { t, i18n } = useTranslation("static");
  const location = useLocation();
  const {
    sidebarCollapsed,
    setSidebarCollapsed,
    darkMode,
    setDarkMode,
    language,
    setLanguage,
  } = useAppStore();

  const sidebarEntries = useMemo(() => getStaticSidebarConfig(t), [t]);

  const breadcrumbs = useMemo(
    () => buildBreadcrumbs(routesConfig, location.pathname, t),
    [location.pathname, t],
  );

  const languages = useMemo(
    () =>
      LANGUAGE_OPTIONS.map((option) => ({
        code: option.code,
        label: option.nativeName,
      })),
    [],
  );

  const handleLanguageChange = useCallback(
    (code: string) => {
      setLanguage(code as typeof language);
      void i18n.changeLanguage(code);
    },
    [setLanguage, i18n],
  );

  return (
    <WorkspaceLayout
      appName={appConfig.projectName}
      // Unconditional: this public shell shows the deployment's own logo even on a
      // first-party build, where `appName` still names a preset for everything else.
      {...configuredLogoSlots(darkMode)}
      sidebarConfig={{
        items: sidebarEntries,
        currentPath: location.pathname,
        indexPath: STATIC_BASE_PATH,
        LinkComponent: TanstackRouterLink,
        allowCollapsible: true,
        // The newer `footerBranding={false}` prop only lands in dashboard-ui-components
        // 0.11; on 0.10.x this flag is the only way to drop the footer branding.
        hideCompanyBranding: true,
      }}
      headerConfig={{
        breadcrumbs,
        collapseBreadcrumbsOnMobile: true,
      }}
      footerConfig={{
        hidden: appConfig.hideFooter,
      }}
      sidebarOpen={!sidebarCollapsed}
      defaultSidebarOpen={true}
      onSidebarOpenChange={(open) => setSidebarCollapsed(!open)}
      onColorThemeChange={(theme) => setDarkMode(theme === "dark")}
      colorTheme={darkMode ? "dark" : "light"}
      languages={languages}
      currentLanguage={language}
      onLanguageChange={handleLanguageChange}
    >
      <Outlet />
    </WorkspaceLayout>
  );
}
