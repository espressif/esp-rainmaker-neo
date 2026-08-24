/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Save, X } from "lucide-react";
import { Alert, Form } from "@espressif/dashboard-ui-components/components";
import { FormFooterActions } from "@/components/form-footer-actions";
import { SmartThingsConfigFields } from "./_components/smartthings-config-fields";
import { SmartThingsDeleteButton } from "./_components/smartthings-delete-button";
import { useSmartThingsConfigForm } from "./use-smartthings-config-form";
import type { SmartThingsConfigFormProps } from "./smartthings-config-form.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

export default function SmartThingsConfigForm({
  initialData,
  onCancel,
  onSuccess,
}: SmartThingsConfigFormProps) {
  const { t } = useTranslation(["voice-assistants", "common"]);
  const {
    form,
    submit,
    isSubmitting,
    submitErrorMessage,
    handleDelete,
    isDeleting,
    deleteErrorMessage,
    canDelete,
  } = useSmartThingsConfigForm({ initialData, onSuccess });

  return (
    <Form {...form}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-6">
        <SmartThingsConfigFields />

        {submitErrorMessage ? (
          <Alert type="error" variant="soft" color="error">
            {submitErrorMessage}
          </Alert>
        ) : null}

        <FormFooterActions
          destructiveAction={
            canDelete ? (
              <SmartThingsDeleteButton
                onConfirm={handleDelete}
                isDeleting={isDeleting}
                disabled={isSubmitting}
                error={deleteErrorMessage}
              />
            ) : undefined
          }
          softAction={{
            label: t("common:actions.cancel", "Cancel"),
            startIcon: <X className={ICON_CLASS} aria-hidden />,
            onClick: onCancel,
            type: "button",
            disabled: isSubmitting || isDeleting,
          }}
          primaryAction={{
            label: t("smartthings.form.updateButton", "Update"),
            startIcon: <Save className={ICON_CLASS} aria-hidden />,
            type: "submit",
            loading: isSubmitting,
            disabled: isDeleting,
          }}
        />
      </form>
    </Form>
  );
}
