/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Languages, MoveRight } from "lucide-react";
import {
  Button,
  IconTextActionCard,
} from "@espressif/dashboard-ui-components/components";
import { getLanguageOption } from "@/config/language.config";
import { useAppStore } from "@/stores/app.store";
import type { LanguageCardProps } from "./language-card.props";

/**
 * Shows the language currently in use and hands off to the picker.
 *
 * The title is the endonym (`English`, `中文`) rather than the translated name so it reads
 * the same whichever locale is active. An unrecognised stored code — a stale persisted value
 * or a bad `?hl=` param — falls back to the raw code instead of rendering blank.
 *
 * Supplying `actions` (rather than `onClick`) keeps the card itself inert, so the link button
 * is the single interactive element and the library's auto-chevron is suppressed.
 */
export default function LanguageCard({ onChangeLanguage }: LanguageCardProps) {
  const { t } = useTranslation("account-settings");
  const language = useAppStore((state) => state.language);
  const languageOption = getLanguageOption(language);

  return (
    <IconTextActionCard
      icon={<Languages className="h-5 w-5" aria-hidden />}
      title={languageOption?.nativeName ?? language}
      description={t(
        "preferences.language.description",
        "Language used across the dashboard.",
      )}
      color="mist"
      variant="outline"
      actions={
        <Button
          variant="link"
          fullWidth={false}
          onClick={onChangeLanguage}
          endIcon={<MoveRight className="h-4 w-4" aria-hidden />}
          animateEndIconOnHover
          size="sm"
        >
          {t("preferences.language.changeButton", "Change Language")}
        </Button>
      }
    />
  );
}
