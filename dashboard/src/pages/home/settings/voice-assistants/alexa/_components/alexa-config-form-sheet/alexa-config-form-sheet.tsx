/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { CustomIcon } from "@/components/custom-icon";
import { TableRowDetailSheet } from "@/components/table-row-detail-sheet";
import { AlexaConfigForm } from "../alexa-config-form";
import type { AlexaConfigFormSheetProps } from "./alexa-config-form-sheet.props";

/**
 * Thin container that hosts {@link AlexaConfigForm} in a right-side sheet. All
 * sheet coupling lives here, so the form stays reusable in a dialog or page.
 * The title reflects whether we are editing an existing config or setting one up.
 */
export default function AlexaConfigFormSheet({
  initialData,
  onClose,
}: AlexaConfigFormSheetProps) {
  const { t } = useTranslation("voice-assistants");
  const hasConfig = Boolean(
    initialData?.client_id ||
      initialData?.skill_id ||
      (initialData?.redirect_uris?.length ?? 0) > 0,
  );

  const labelText = hasConfig
    ? t("alexa.form.editTitle", "Edit Alexa configuration")
    : t("alexa.form.configureTitle", "Configure Alexa");

  const label = (
    <span className="flex items-center gap-2">
      <CustomIcon type="amazon-alexa" size={24} aria-hidden />
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
      <AlexaConfigForm
        initialData={initialData}
        onCancel={onClose}
        onSuccess={onClose}
      />
    </TableRowDetailSheet>
  );
}
