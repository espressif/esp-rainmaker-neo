/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { createRootRoute, Outlet } from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { I18nextProvider } from "react-i18next";
import {
  Toaster,
  TooltipProvider,
} from "@espressif/dashboard-ui-components/components";
import { Suspense, useEffect } from "react";
import { queryClient } from "@/api/query-client";
import i18n from "@/i18n/config";
import ErrorBoundary from "@/components/error/ErrorBoundary";
import AppBootstrap from "@/containers/app-bootstrap/app-bootstrap";
import { useAppStore } from "@/stores/app.store";
import { appConfig, type SupportedLanguage } from "@/lib/app-config";
import urlParamsConfig from "@/config/url-params.config.json";
import { getURLParamValue } from "@/utils/utils";
import "@/styles/globals.css";

export const rootRoute = createRootRoute({
  component: RootComponent,
});

function RootComponent() {
  const { darkMode, language, setSidebarCollapsed, setDarkMode, setLanguage } =
    useAppStore();

  // Apply URL params after component mounts (overrides persisted values if URL params exist)
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);

    if (params.has(urlParamsConfig.SIDEBAR_COLLAPSED)) {
      const value = getURLParamValue(urlParamsConfig.SIDEBAR_COLLAPSED);
      setSidebarCollapsed(value === "true");
    }
    if (params.has(urlParamsConfig.DARK_MODE)) {
      const value = getURLParamValue(urlParamsConfig.DARK_MODE);
      setDarkMode(value === "true");
    }
    if (params.has(urlParamsConfig.LANGUAGE)) {
      const value = getURLParamValue(urlParamsConfig.LANGUAGE) as
        | SupportedLanguage
        | undefined;
      if (value && appConfig.i18n.supportedLanguages.includes(value)) {
        setLanguage(value);
      }
    }
  }, [setSidebarCollapsed, setDarkMode, setLanguage]);

  // Sync i18next language with store
  useEffect(() => {
    if (i18n.language !== language) {
      void i18n.changeLanguage(language);
    }
  }, [language]);

  // Apply darkMode to document
  useEffect(() => {
    if (darkMode) {
      document.documentElement.classList.add("dark");
    } else {
      document.documentElement.classList.remove("dark");
    }
  }, [darkMode]);

  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          {/* Library components that reveal a tooltip on overflow (SectionCard's
              secondaryText, truncated list rows) read a strict global context,
              so the provider has to sit above every route. */}
          <TooltipProvider>
            <AppBootstrap>
              <Suspense
                fallback={
                  <div className="flex min-h-screen items-center justify-center">
                    Loading...
                  </div>
                }
              >
                <Outlet />
              </Suspense>
            </AppBootstrap>
            <Toaster position="top-center" toastVariant="solid" />
          </TooltipProvider>
        </I18nextProvider>
      </QueryClientProvider>
    </ErrorBoundary>
  );
}
