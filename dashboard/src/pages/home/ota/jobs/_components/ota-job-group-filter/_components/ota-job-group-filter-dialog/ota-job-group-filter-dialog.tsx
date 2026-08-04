/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@espressif/dashboard-ui-components/components";
import { OtaJobGroupFilterBody } from "../ota-job-group-filter-body";
import type { OtaJobGroupFilterDialogProps } from "./ota-job-group-filter-dialog.props";

/**
 * Modal container for the OTA jobs node-group filter — header and sizing only;
 * the draft lives in the trigger and the selection UI in the body. Dismissal is
 * deliberate (no outside click, no Escape) so a half-made selection is never
 * lost to a stray click.
 */
export function OtaJobGroupFilterDialog({
  open,
  onOpenChange,
  value,
  onValueChange,
  onApply,
  onCancel,
}: OtaJobGroupFilterDialogProps) {
  const { t } = useTranslation("ota-jobs");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        onInteractOutside={(event) => event.preventDefault()}
        onEscapeKeyDown={(event) => event.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>
            {t("filters.groupModalTitle", "Filter by node group")}
          </DialogTitle>
          <DialogDescription>
            {t(
              "filters.groupModalDescription",
              "Show only OTA jobs targeting the selected node group.",
            )}
          </DialogDescription>
        </DialogHeader>

        <OtaJobGroupFilterBody
          value={value}
          onValueChange={onValueChange}
          onApply={onApply}
          onCancel={onCancel}
        />
      </DialogContent>
    </Dialog>
  );
}
