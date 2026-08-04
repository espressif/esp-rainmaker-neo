/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Palette } from "lucide-react";
import {
  SectionCard,
  SelectableCardList,
} from "@espressif/dashboard-ui-components/components";
import type { SelectableCardListItem } from "@espressif/dashboard-ui-components/components";
import {
  COLOR_THEME_OPTIONS,
  getColorThemeId,
  isDarkColorTheme,
} from "@/config/color-theme.config";
import { useAppStore } from "@/stores/app.store";

/**
 * Colour theme picker. Reads and writes `darkMode` on the app store directly — it is
 * client-only UI state, so there is nothing to prop-drill and nothing to fetch.
 *
 * The store is the only place the choice lives; `__root.tsx` reacts to it by toggling the
 * `dark` class on `<html>`, which is what makes the whole app (this card included) repaint
 * the instant a row is selected.
 */
export default function ColorThemeCard() {
  const { t } = useTranslation("account-settings");
  const darkMode = useAppStore((state) => state.darkMode);
  const setDarkMode = useAppStore((state) => state.setDarkMode);

  const handleColorThemeChange = useCallback(
    (id: string) => setDarkMode(isDarkColorTheme(id)),
    [setDarkMode],
  );

  const data = useMemo<SelectableCardListItem[]>(
    () =>
      COLOR_THEME_OPTIONS.map((option) => {
        const Icon = option.icon;
        return {
          id: option.id,
          icon: <Icon className="h-5 w-5" aria-hidden />,
          primaryText: t(option.labelKey, option.fallback),
          secondaryText: t(option.descriptionKey, option.descriptionFallback),
        };
      }),
    [t],
  );

  const label = t("preferences.colorTheme.title", "Color theme");

  return (
    <SectionCard
      icon={<Palette className="h-5 w-5" aria-hidden />}
      primaryText={label}
      secondaryText={t(
        "preferences.colorTheme.description",
        "Choose how the dashboard looks on this device.",
      )}
      allowCollapse={false}
      color="mist"
      variant="outline"
    >
      <SelectableCardList
        data={data}
        allowMultiple={false}
        value={getColorThemeId(darkMode)}
        onChange={handleColorThemeChange}
        aria-label={label}
        size="sm"
      />
    </SectionCard>
  );
}
