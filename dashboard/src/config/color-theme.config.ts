/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react";
import { Moon, Sun } from "lucide-react";

/** Identifier of a selectable colour theme. Mirrors the library's `colorTheme` prop. */
export type ColorThemeId = "light" | "dark";

export type ColorThemeOption = {
  id: ColorThemeId;
  /** Identity icon shown in the selectable card row. */
  icon: ComponentType<{ className?: string }>;
  /** i18n key under the `account-settings` namespace. */
  labelKey: string;
  /** English fallback used until the key is translated. */
  fallback: string;
  descriptionKey: string;
  descriptionFallback: string;
};

/**
 * Single source of truth for the colour theme choices offered on the App preferences page.
 *
 * The persisted primitive stays `darkMode: boolean` in
 * [app.store.ts](../stores/app.store.ts) — this list is presentation only, so there is
 * exactly one stored value for the concept. Use {@link getColorThemeId} to project the
 * boolean onto an id and {@link isDarkColorTheme} to project an id back.
 */
export const COLOR_THEME_OPTIONS = [
  {
    id: "light",
    icon: Sun,
    labelKey: "account-settings:preferences.colorTheme.light.label",
    fallback: "Light",
    descriptionKey: "account-settings:preferences.colorTheme.light.description",
    descriptionFallback: "Bright surfaces with dark text.",
  },
  {
    id: "dark",
    icon: Moon,
    labelKey: "account-settings:preferences.colorTheme.dark.label",
    fallback: "Dark",
    descriptionKey: "account-settings:preferences.colorTheme.dark.description",
    descriptionFallback: "Dimmed surfaces that are easier on the eyes at night.",
  },
] as const satisfies readonly ColorThemeOption[];

export function getColorThemeId(darkMode: boolean): ColorThemeId {
  return darkMode ? "dark" : "light";
}

/**
 * Projects a selected card id back onto the stored boolean. Anything other than `dark`
 * resolves to light rather than throwing, so an unknown id can never break the page.
 */
export function isDarkColorTheme(id: string): boolean {
  return id === "dark";
}
