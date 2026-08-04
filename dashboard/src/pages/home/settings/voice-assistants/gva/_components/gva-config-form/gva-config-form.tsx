/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Save, X } from "lucide-react";
import { Alert, Form } from "@espressif/dashboard-ui-components/components";
import { FormFooterActions } from "@/components/form-footer-actions";
import { GvaEditMethodField } from "./_components/gva-edit-method-field";
import { GvaJsonUploadField } from "./_components/gva-json-upload-field";
import { GvaConfigFields } from "./_components/gva-config-fields";
import { GvaRedirectUrisField } from "./_components/gva-redirect-uris-field";
import { GvaDeleteButton } from "./_components/gva-delete-button";
import { useGvaConfigForm } from "./use-gva-config-form";
import type { GvaConfigFormProps } from "./gva-config-form.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

/**
 * Self-contained GVA configuration form. Owns its schema, state and mutations;
 * it is unaware of the container it lives in (sheet / dialog / page) and only
 * exposes `onCancel` / `onSuccess`. The same POST endpoint handles both
 * first-time configuration and edits.
 */
export default function GvaConfigForm({
  initialData,
  onCancel,
  onSuccess,
}: GvaConfigFormProps) {
  const { t } = useTranslation(["voice-assistants", "common"]);
  const {
    form,
    submit,
    isSubmitting,
    submitErrorMessage,
    editMethod,
    setEditMethod,
    fileName,
    fileError,
    handleFileChange,
    handleDelete,
    isDeleting,
    deleteErrorMessage,
    canDelete,
  } = useGvaConfigForm({ initialData, onSuccess });

  return (
    <Form {...form}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-6">
        <GvaEditMethodField value={editMethod} onChange={setEditMethod} />

        {editMethod === "upload" ? (
          <GvaJsonUploadField
            fileName={fileName}
            fileError={fileError}
            onFilesChange={(files) => void handleFileChange(files)}
          />
        ) : (
          <GvaConfigFields />
        )}

        <GvaRedirectUrisField uris={initialData?.redirect_uris ?? []} />

        {submitErrorMessage ? (
          <Alert type="error" variant="soft" color="error">
            {submitErrorMessage}
          </Alert>
        ) : null}

        <FormFooterActions
          destructiveAction={
            canDelete ? (
              <GvaDeleteButton
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
            label: t("gva.form.updateButton", "Update"),
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
