/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react";
import { Languages } from "lucide-react";
import { SUPPORTED_LANGUAGES, type SupportedLanguage } from "@/lib/constants";

export type LanguageOption = {
  code: SupportedLanguage;
  /**
   * Endonym — the language's name in itself. Deliberately not translated: it must read the
   * same in every locale so a user stranded in a language they cannot read still finds theirs.
   */
  nativeName: string;
  /** Key in the `common` namespace for the name in the *active* locale. */
  labelKey: string;
  /** English fallback used until the key is translated. */
  fallback: string;
  /** Identity icon shown in the selectable card row. */
  icon: ComponentType<{ className?: string }>;
};

/**
 * Single source of truth for language presentation. Keyed by code so the compiler rejects
 * the file the moment a locale is added to `SUPPORTED_LANGUAGES` without an entry here.
 *
 * Adding a locale therefore means three edits: the tuple in
 * [constants.ts](../lib/constants.ts), a `resources` entry in
 * [i18n/config.ts](../i18n/config.ts), and one entry below.
 */
const LANGUAGE_PRESENTATION: Record<
  SupportedLanguage,
  Omit<LanguageOption, "code">
> = {
  en: {
    nativeName: "English",
    labelKey: "common:languages.en",
    fallback: "English",
    icon: Languages,
  },
  zh: {
    nativeName: "中文",
    labelKey: "common:languages.zh",
    fallback: "Chinese",
    icon: Languages,
  },
};

/**
 * Ordered list for iteration — the App preferences picker and the header account menu both
 * render from this, so the two surfaces can never disagree on which languages exist.
 * Display order comes from `SUPPORTED_LANGUAGES` because object key order is not a contract.
 */
export const LANGUAGE_OPTIONS: readonly LanguageOption[] = SUPPORTED_LANGUAGES.map(
  (code) => ({ code, ...LANGUAGE_PRESENTATION[code] }),
);

export function isSupportedLanguage(value: string): value is SupportedLanguage {
  return value in LANGUAGE_PRESENTATION;
}

/**
 * Resolves a language code to its presentation entry. Returns `null` for anything
 * unsupported so callers can fall back to rendering the raw code — a stale persisted or
 * URL-supplied language must never crash the page.
 */
export function getLanguageOption(code: string): LanguageOption | null {
  if (!isSupportedLanguage(code)) {
    return null;
  }

  return { code, ...LANGUAGE_PRESENTATION[code] };
}
