/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { TableRowDetailSheet } from "@/components/table-row-detail-sheet";
import BrandingForm from "../branding-form";
import type { BrandingFormSheetProps } from "./branding-form-sheet.props";

export default function BrandingFormSheet({ onClose }: BrandingFormSheetProps) {
  const { t } = useTranslation("branding");

  return (
    <TableRowDetailSheet
      contentClassName="w-screen max-w-screen-md"
      label={t("form.replaceTitle", "Replace the brand")}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <BrandingForm onCancel={onClose} onSuccess={onClose} />
    </TableRowDetailSheet>
  );
}
