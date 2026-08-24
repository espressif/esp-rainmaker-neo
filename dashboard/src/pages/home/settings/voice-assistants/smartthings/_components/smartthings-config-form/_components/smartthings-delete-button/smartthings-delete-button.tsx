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
import type { SmartThingsDeleteButtonProps } from "./smartthings-delete-button.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

export default function SmartThingsDeleteButton({
  onConfirm,
  isDeleting,
  disabled,
  error,
}: SmartThingsDeleteButtonProps) {
  const { t } = useTranslation(["voice-assistants", "common"]);

  return (
    <ConfirmationDialog
      title={t("smartthings.form.delete.title", "Delete SmartThings configuration?")}
      description={t(
        "smartthings.form.delete.description",
        "This permanently removes the SmartThings Schema App credentials. Linked accounts stop working until it is configured again.",
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
