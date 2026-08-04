/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { CustomIcon } from "@/components/custom-icon";
import { TableRowDetailSheet } from "@/components/table-row-detail-sheet";
import { GvaConfigForm } from "../gva-config-form";
import type { GvaConfigFormSheetProps } from "./gva-config-form-sheet.props";

/**
 * Thin container that hosts {@link GvaConfigForm} in a right-side sheet. All
 * sheet coupling lives here, so the form stays reusable in a dialog or page. The
 * title reflects whether we are editing an existing config or setting one up.
 */
export default function GvaConfigFormSheet({
  initialData,
  onClose,
}: GvaConfigFormSheetProps) {
  const { t } = useTranslation("voice-assistants");
  const hasConfig = Boolean(
    initialData?.project_id || initialData?.client_email,
  );

  const labelText = hasConfig
    ? t("gva.form.editTitle", "Edit GVA configuration")
    : t("gva.form.configureTitle", "Configure GVA");

  const label = (
    <span className="flex items-center gap-2">
      <CustomIcon type="google-assistant" size={24} aria-hidden />
      {labelText}
    </span>
  );

  return (
    <TableRowDetailSheet
      contentClassName="w-screen max-w-screen-md"
      label={label}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <GvaConfigForm
        initialData={initialData}
        onCancel={onClose}
        onSuccess={onClose}
      />
    </TableRowDetailSheet>
  );
}
