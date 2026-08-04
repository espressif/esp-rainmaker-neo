/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Languages } from "lucide-react";
import {
  SearchBox,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import type { SelectableCardListItem } from "@espressif/dashboard-ui-components/components";
import {
  LANGUAGE_OPTIONS,
  isSupportedLanguage,
} from "@/config/language.config";
import { useAppStore } from "@/stores/app.store";
import { LanguagePickerBody } from "./_components/language-picker-body";
import { matchesLanguageSearch } from "./language-picker.utils";

/**
 * Language chooser. Owns no container — no `Sheet`, no `Dialog`, no title of its own beyond
 * the card's — so it can be hosted in a sheet today and a dialog or a plain page tomorrow.
 *
 * Selecting a row persists immediately and does **not** dismiss anything: the store write
 * flows through `__root.tsx` into `i18n.changeLanguage`, so this card's own labels re-render
 * in the newly chosen language. That is the confirmation the user gets.
 */
export default function LanguagePicker() {
  const { t } = useTranslation("account-settings");
  const language = useAppStore((state) => state.language);
  const setLanguage = useAppStore((state) => state.setLanguage);
  const [searchTerm, setSearchTerm] = useState("");

  const handleClearSearch = useCallback(() => setSearchTerm(""), []);

  // `onChange` hands back a bare string, so the guard both narrows the type and drops an
  // unrecognised id rather than persisting a language with no translations behind it.
  const handleSelectLanguage = useCallback(
    (code: string) => {
      if (!isSupportedLanguage(code)) {
        return;
      }
      setLanguage(code);
    },
    [setLanguage],
  );

  const items = useMemo<SelectableCardListItem[]>(() => {
    const normalizedSearchTerm = searchTerm.trim().toLowerCase();

    // flatMap rather than filter+map so the translated label is resolved once per language and
    // reused for both the search haystack and the row's secondary line.
    return LANGUAGE_OPTIONS.flatMap((option) => {
      const translatedLabel = t(`common:${option.labelKey}`, option.fallback);

      if (!matchesLanguageSearch(option, normalizedSearchTerm, translatedLabel)) {
        return [];
      }

      const Icon = option.icon;
      return [
        {
          id: option.code,
          icon: <Icon className="h-5 w-5" aria-hidden />,
          primaryText: option.nativeName,
          secondaryText: translatedLabel,
        },
      ];
    });
  }, [searchTerm, t]);

  const label = t("preferences.language.picker.title", "Choose a language");

  return (
    <SectionCard
      icon={<Languages className="h-5 w-5" aria-hidden />}
      primaryText={label}
      secondaryText={t(
        "preferences.language.picker.description",
        "Applies as soon as you pick one.",
      )}
      allowCollapse={false}
      color="silver"
      variant="outline"
      size="default"
      actions={
        /*
          SearchBox is uncontrolled and only fires `onSearch` on Enter, so `onValueChange` is
          wired to the same setter to filter as the user types.
        */
        <SearchBox
          size="sm"
          className="w-full sm:w-[180px]"
          placeholder={t(
            "preferences.language.picker.searchPlaceholder",
            "Search languages",
          )}
          onValueChange={setSearchTerm}
          onSearch={setSearchTerm}
          onClear={handleClearSearch}
        />
      }
    >
      <LanguagePickerBody
        items={items}
        value={language}
        onChange={handleSelectLanguage}
        ariaLabel={label}
      />
    </SectionCard>
  );
}
