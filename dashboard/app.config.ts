/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { defineConfig } from "./src/lib/app-config/define-config.ts";

export default defineConfig({
  title: "ESP RainMaker Neo | Admin Dashboard",
  description: "Manage devices, users, and deployments for ESP RainMaker Neo.",
  themeColor: { light: "#FAFCFF", dark: "#030303" },
  projectName: "rainmaker-neo",
  storagePrefix: "espdashboard",
  defaults: {
    sidebarCollapsed: false,
    darkMode:
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches,
    language: "en",
  },
  logo: {
    minimal: {
      light: { src: "assets/img/logo/logo-no-text.svg", width: 400, height: 400 },
      dark: { src: "assets/img/logo/logo-no-text-dm.svg", width: 400, height: 400 },
    },
    full: {
      light: { src: "assets/img/logo/logo-with-text.svg", width: 363.31, height: 48 },
      dark: { src: "assets/img/logo/logo-with-text-dm.svg", width: 363.31, height: 48 },
    },
  },
  favicon: {
    ico: "assets/img/favicon/favicon.ico",
  },
  i18n: {
    supportedLanguages: ["en", "zh"],
  },
  hideFooter: true,
  oAuth: false,
  customAuth: {
    allowSignups: false,
    allowKeepMeSignedIn: true,
    onboardingLayoutBackgroundImage: "assets/img/backgrounds/e22-home-banner.webp",
    onboardingHeading: "ESP RainMaker <gradient-text>NEO</gradient-text> Admin Dashboard",
  },
});
