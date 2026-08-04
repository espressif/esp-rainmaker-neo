/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  Alert,
  SelectableCardList,
} from "@espressif/dashboard-ui-components/components";
import type { LanguagePickerBodyProps } from "./language-picker-body.props";

/**
 * Renders exactly one of the picker's two states via early returns, so neither branch is
 * hidden inside a ternary in the card body.
 */
export default function LanguagePickerBody({
  items,
  value,
  onChange,
  ariaLabel,
}: LanguagePickerBodyProps) {
  const { t } = useTranslation("account-settings");

  if (items.length === 0) {
    return (
      <Alert type="info" variant="soft" color="info">
        {t(
          "preferences.language.picker.noResults",
          "No languages match your search.",
        )}
      </Alert>
    );
  }

  return (
    <SelectableCardList
      data={items}
      allowMultiple={false}
      value={value}
      onChange={onChange}
      aria-label={ariaLabel}
      size="sm"
    />
  );
}
