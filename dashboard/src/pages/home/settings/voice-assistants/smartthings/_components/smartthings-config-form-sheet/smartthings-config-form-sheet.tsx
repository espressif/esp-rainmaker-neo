/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { CustomIcon } from "@/components/custom-icon";
import { TableRowDetailSheet } from "@/components/table-row-detail-sheet";
import { SmartThingsConfigForm } from "../smartthings-config-form";
import type { SmartThingsConfigFormSheetProps } from "./smartthings-config-form-sheet.props";

export default function SmartThingsConfigFormSheet({
  initialData,
  onClose,
}: SmartThingsConfigFormSheetProps) {
  const { t } = useTranslation("voice-assistants");
  const hasConfig = Boolean(initialData?.client_id);

  const labelText = hasConfig
    ? t("smartthings.form.editTitle", "Edit SmartThings configuration")
    : t("smartthings.form.configureTitle", "Configure SmartThings");

  const label = (
    <span className="flex items-center gap-2">
      <CustomIcon type="smartthings" size={24} aria-hidden />
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
      <SmartThingsConfigForm
        initialData={initialData}
        onCancel={onClose}
        onSuccess={onClose}
      />
    </TableRowDetailSheet>
  );
}
