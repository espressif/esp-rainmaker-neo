/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Trash2 } from "lucide-react";
import {
  Button,
  ConfirmationDialog,
} from "@espressif/dashboard-ui-components/components";
import type { GvaDeleteButtonProps } from "./gva-delete-button.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

/**
 * Destructive delete trigger wrapped in a confirmation dialog. `onConfirm` is
 * allowed to throw so the dialog stays open and surfaces `error` on failure;
 * on success the dialog closes and the parent handles the toast + sheet close.
 */
export default function GvaDeleteButton({
  onConfirm,
  isDeleting,
  disabled,
  error,
}: GvaDeleteButtonProps) {
  const { t } = useTranslation(["voice-assistants", "common"]);

  return (
    <ConfirmationDialog
      title={t("gva.form.delete.title", "Delete GVA configuration?")}
      description={t(
        "gva.form.delete.description",
        "This permanently removes the Google Voice Assistant configuration. This action cannot be undone.",
      )}
      confirmButtonText={t("common:actions.delete", "Delete")}
      cancelButtonText={t("common:actions.cancel", "Cancel")}
      confirmButtonColor="error"
      onConfirm={onConfirm}
      onCancel={() => {}}
      isLoading={isDeleting}
      error={error}
    >
      <Button
        type="button"
        variant="outline"
        color="error"
        size="lg"
        fullWidth={false}
        className="w-full sm:w-auto"
        disabled={disabled}
        startIcon={<Trash2 className={ICON_CLASS} aria-hidden />}
      >
        {t("common:actions.delete", "Delete")}
      </Button>
    </ConfirmationDialog>
  );
}
