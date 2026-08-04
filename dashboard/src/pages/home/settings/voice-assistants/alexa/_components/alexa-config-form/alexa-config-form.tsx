/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Save, X } from "lucide-react";
import { Alert, Form } from "@espressif/dashboard-ui-components/components";
import { FormFooterActions } from "@/components/form-footer-actions";
import { AlexaConfigFields } from "./_components/alexa-config-fields";
import { RedirectUrisField } from "./_components/redirect-uris-field";
import { useAlexaConfigForm } from "./use-alexa-config-form";
import type { AlexaConfigFormProps } from "./alexa-config-form.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

/**
 * Self-contained Alexa configuration form. Owns its schema, state and mutation;
 * it is unaware of the container it lives in (sheet / dialog / page) and only
 * exposes `onCancel` / `onSuccess` callbacks. The same POST endpoint handles
 * both first-time configuration and edits.
 */
export default function AlexaConfigForm({
  initialData,
  onCancel,
  onSuccess,
}: AlexaConfigFormProps) {
  const { t } = useTranslation(["voice-assistants", "common"]);
  const { form, submit, isSubmitting, submitErrorMessage } = useAlexaConfigForm({
    initialData,
    onSuccess,
  });

  return (
    <Form {...form}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-6">
        <AlexaConfigFields />
        <RedirectUrisField />

        {submitErrorMessage ? (
          <Alert type="error" variant="soft" color="error">
            {submitErrorMessage}
          </Alert>
        ) : null}

        <FormFooterActions
          softAction={{
            label: t("common:actions.cancel", "Cancel"),
            startIcon: <X className={ICON_CLASS} aria-hidden />,
            onClick: onCancel,
            type: "button",
            disabled: isSubmitting,
          }}
          primaryAction={{
            label: t("alexa.form.updateButton", "Update"),
            startIcon: <Save className={ICON_CLASS} aria-hidden />,
            type: "submit",
            loading: isSubmitting,
          }}
        />
      </form>
    </Form>
  );
}
