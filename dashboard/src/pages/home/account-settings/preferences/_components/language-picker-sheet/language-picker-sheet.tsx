/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Languages } from "lucide-react";
import { TableRowDetailSheet } from "@/components/table-row-detail-sheet";
import { LanguagePicker } from "../language-picker";
import type { LanguagePickerSheetProps } from "./language-picker-sheet.props";

/**
 * Thin container that hosts {@link LanguagePicker} in a right-side sheet. All sheet coupling
 * lives here, so the picker stays reusable in a dialog or inline on a page.
 *
 * `max-w-lg` narrows the wrapper's screen-wide default — a short language list does not want a
 * full-width sheet. The top-right close button comes from `SheetContent`, which renders it by
 * default.
 */
export default function LanguagePickerSheet({
  onClose,
}: LanguagePickerSheetProps) {
  const { t } = useTranslation("account-settings");

  const label = (
    <span className="flex items-center gap-2">
      <Languages className="h-5 w-5 shrink-0" aria-hidden />
      {t("preferences.language.sheetTitle", "Change language")}
    </span>
  );

  return (
    <TableRowDetailSheet
      contentClassName="w-screen max-w-2xl"
      label={label}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <LanguagePicker />
    </TableRowDetailSheet>
  );
}
