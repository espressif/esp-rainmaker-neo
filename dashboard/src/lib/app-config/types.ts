/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { AppName } from '@espressif/dashboard-ui-components';
import type { SupportedLanguage } from '../constants';

/**
 * Logo asset configuration
 */
export interface LogoAsset {
  src: string;
  width: number;
  height: number;
}

/**
 * Logo variant with light and dark theme versions
 */
export interface LogoVariant {
  light: LogoAsset;
  dark: LogoAsset;
}

/**
 * Application default settings
 */
export interface AppDefaults {
  sidebarCollapsed: boolean;
  darkMode: boolean;
  language: SupportedLanguage;
}

/**
 * Internationalization configuration
 */
export interface I18nConfig {
  supportedLanguages: readonly SupportedLanguage[];
}

/**
 * Browser icon set. A bare string is shorthand for `{ ico: '<path>' }`.
 *
 * `svg` is preferred by Chrome/Firefox/Edge and stays crisp at any density;
 * `ico` remains the fallback for Safari, which ignores SVG icons entirely.
 * Both are resolved relative to `src/` and injected into index.html at build
 * time. The union requires at least one, so `{}` is a compile error rather
 * than a page that silently ships no icon.
 */
export type FaviconConfig =
  | string
  | { ico: string; svg?: string }
  | { ico?: string; svg: string };

/**
 * Browser chrome colour per colour scheme (`<meta name="theme-color">`).
 * Should match `--color-background` in the corresponding theme.
 */
export interface ThemeColorConfig {
  light: string;
  dark: string;
}

/**
 * Custom (non-OAuth) authentication configuration
 */
export interface CustomAuthConfig {
  /** Allow new user signups via the custom auth flow */
  allowSignups: boolean;
  /** Show "Keep me signed in" checkbox on the login page */
  allowKeepMeSignedIn: boolean;
  /** Background image path for the onboarding layout right panel */
  onboardingLayoutBackgroundImage?: string;
  /** Heading text displayed over the onboarding layout right panel */
  onboardingHeading?: string;
}

/**
 * Main application configuration
 */
export interface AppConfig {
  /** Application title displayed in browser tab and header */
  title: string;
  /** Meta description. Surfaces in bookmark/history UI and link unfurls. */
  description?: string;
  /** Browser chrome colour per colour scheme */
  themeColor?: ThemeColorConfig;
  /**
   * Logo preset key from `@espressif/dashboard-ui-components`.
   *
   * **Omit it when forking this project.** The union only covers Espressif's own
   * products, so a downstream deployment has no valid value to put here — leaving it
   * unset is the supported path: every layout then renders the `logo` assets below
   * instead of a preset. Set it only for a first-party build.
   */
  projectName?: AppName;
  /** Prefix for localStorage keys */
  storagePrefix: string;
  /** Default application settings */
  defaults: AppDefaults;
  /**
   * Logo assets, rendered through `components/brand-logo`.
   *
   * The public static shell always uses these. Everywhere else they are the fallback
   * for when `projectName` is unset — so a fork rebrands the whole app by dropping
   * `projectName` and pointing these at its own files, with no code change.
   */
  logo?: {
    minimal: LogoVariant;
    full: LogoVariant;
  };
  /** Browser icon set, injected into index.html at build time */
  favicon: FaviconConfig;
  /** Internationalization settings */
  i18n: I18nConfig;
  /** Hide the footer component */
  hideFooter: boolean;
  /** Use OAuth (PKCE) for authentication. Defaults to true. */
  oAuth?: boolean;
  /** Custom auth settings (only used when oAuth is false) */
  customAuth?: CustomAuthConfig;
}
