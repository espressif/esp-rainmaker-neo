/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";
import { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import {
  EntryLayout,
  type EntryLayoutProps,
} from "@espressif/dashboard-ui-components/layouts";
import { appConfig } from "@/lib/app-config";
import { resolveAssetPath } from "@/lib/asset-resolver";
import { checkAuthStatus } from "@/lib/auth";
import { useAppStore } from "@/stores/app.store";
import { presetFallbackLogoAssets } from "@/components/brand-logo";

export type OnboardingLayoutProps = Omit<
  EntryLayoutProps,
  "appName" | "heading"
> & {
  heading?: ReactNode;
  children: ReactNode;
};

function defaultBackgroundImageUrl(): string | undefined {
  const path = appConfig.customAuth?.onboardingLayoutBackgroundImage;
  if (!path) {return undefined;}
  return resolveAssetPath(path);
}

export default function OnboardingLayout({
  children,
  heading,
  darkMode: darkModeProp,
  backgroundImageUrl: backgroundImageUrlProp,
  ...rest
}: OnboardingLayoutProps) {
  const navigate = useNavigate();
  const storeDarkMode = useAppStore((state) => state.darkMode);
  const darkMode = darkModeProp ?? storeDarkMode;

  // Same check as the /home guard, so a session it would reject is never sent there and the two pages cannot bounce each other.
  useEffect(() => {
    let cancelled = false;
    void checkAuthStatus().then((isAuthenticated) => {
      if (isAuthenticated && !cancelled) {
        void navigate({ to: "/home", replace: true });
      }
    });
    return () => {
      cancelled = true;
    };
  }, [navigate]);

  const backgroundImageUrl =
    backgroundImageUrlProp ?? defaultBackgroundImageUrl();

  const resolvedHeading =
    heading ?? appConfig.customAuth?.onboardingHeading ?? undefined;

  return (
    <EntryLayout
      appName={appConfig.projectName}
      // `undefined` while `projectName` names a preset; a fork omits it and the entry
      // panel renders the configured assets instead.
      logoAssets={presetFallbackLogoAssets}
      darkMode={darkMode}
      heading={resolvedHeading}
      backgroundImageUrl={backgroundImageUrl}
      {...rest}
    >
      {children}
    </EntryLayout>
  );
}
