/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useState } from "react";
import { ColorThemeCard } from "./_components/color-theme-card";
import { LanguageCard } from "./_components/language-card";
import { LanguagePickerSheet } from "./_components/language-picker-sheet";
import { PreferencesScopeNotice } from "./_components/preferences-scope-notice";

/**
 * App preferences tab. Every card owns its own slice of the app store, so this page holds no
 * preference state itself — only whether the language picker is on screen.
 *
 * The section shell (`account-settings.tsx`) supplies `PageContainer` and the page heading,
 * so tab bodies render bare cards.
 */
export default function Preferences() {
  const [isLanguageSheetOpen, setLanguageSheetOpen] = useState(false);

  const openLanguageSheet = useCallback(() => setLanguageSheetOpen(true), []);
  const closeLanguageSheet = useCallback(() => setLanguageSheetOpen(false), []);

  return (
    <div className="flex flex-col gap-6">
      <PreferencesScopeNotice />
      <ColorThemeCard />
      <LanguageCard onChangeLanguage={openLanguageSheet} />

      {isLanguageSheetOpen && (
        <LanguagePickerSheet onClose={closeLanguageSheet} />
      )}
    </div>
  );
}
