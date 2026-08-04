/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LanguageOption } from "@/config/language.config";

/**
 * Matches a language against the picker's search term. The haystack covers the code, the
 * endonym and the name in the active locale, so "zh", "中文" and "Chinese" all find the same
 * row regardless of which language the UI is currently in.
 *
 * `normalizedSearchTerm` is expected already trimmed and lowercased; the translated label is
 * passed in rather than resolved here so this stays a pure function.
 */
export function matchesLanguageSearch(
  option: LanguageOption,
  normalizedSearchTerm: string,
  translatedLabel: string,
): boolean {
  if (!normalizedSearchTerm) {
    return true;
  }

  const haystack = `${option.code} ${option.nativeName} ${translatedLabel}`;
  return haystack.toLowerCase().includes(normalizedSearchTerm);
}
