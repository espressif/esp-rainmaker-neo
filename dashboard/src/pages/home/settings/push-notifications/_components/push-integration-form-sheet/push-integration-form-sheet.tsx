/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { TableRowDetailSheet } from "@/components/table-row-detail-sheet";
import PushIntegrationForm from "../push-integration-form";
import type { PushIntegrationFormSheetProps } from "./push-integration-form-sheet.props";

/**
 * Thin container that hosts {@link PushIntegrationForm} in a right-side sheet.
 * All sheet coupling lives here, so the form stays reusable in a dialog or page.
 */
export default function PushIntegrationFormSheet({
  onClose,
}: PushIntegrationFormSheetProps) {
  const { t } = useTranslation("push-notifications");

  return (
    <TableRowDetailSheet
      contentClassName="w-screen max-w-screen-md"
      label={t("form.sheetTitle", "Add push notification integration")}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <PushIntegrationForm onCancel={onClose} onSuccess={onClose} />
    </TableRowDetailSheet>
  );
}
