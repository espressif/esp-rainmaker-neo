/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { CustomIcon } from "@/components/custom-icon";
import { TableRowDetailSheet } from "@/components/table-row-detail-sheet";
import { AlexaCreateSkillForm } from "../alexa-create-skill-form";
import type { AlexaCreateSkillFormSheetProps } from "./alexa-create-skill-form-sheet.props";

/**
 * Thin container that hosts {@link AlexaCreateSkillForm} in a right-side sheet, mirroring
 * {@link AlexaConfigFormSheet}. All sheet coupling lives here so the form stays reusable.
 */
export default function AlexaCreateSkillFormSheet({
  onClose,
  onCreated,
}: AlexaCreateSkillFormSheetProps) {
  const { t } = useTranslation("voice-assistants");

  const label = (
    <span className="flex items-center gap-2">
      <CustomIcon type="amazon-alexa" size={24} aria-hidden />
      {t("alexa.autoCreate.title", "Set up Alexa skill")}
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
      <AlexaCreateSkillForm onCancel={onClose} onCreated={onCreated} />
    </TableRowDetailSheet>
  );
}
