/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { BellPlus, X } from "lucide-react";
import { Alert, Form } from "@espressif/dashboard-ui-components/components";
import { FormFooterActions } from "@/components/form-footer-actions";
import IntegrationTypeField from "./_components/integration-type-field";
import ApnsFields from "./_components/apns-fields";
import FcmFields from "./_components/fcm-fields";
import { usePushIntegrationForm } from "./use-push-integration-form";
import type { PushIntegrationFormProps } from "./push-integration-form.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

/**
 * Self-contained "Add push notification integration" form. Owns its schema,
 * state and actions; it is unaware of the container it lives in (sheet / dialog
 * / page) and only exposes `onCancel` / `onSuccess` callbacks.
 */
export default function PushIntegrationForm({
  onCancel,
  onSuccess,
}: PushIntegrationFormProps) {
  const { t } = useTranslation(["push-notifications", "common"]);
  const { form, integrationType, submit, isSubmitting, submitErrorMessage } =
    usePushIntegrationForm({ onSuccess });

  return (
    <Form {...form}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-6">
        <IntegrationTypeField />

        {integrationType === "ios" ? <ApnsFields /> : <FcmFields />}

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
            label: t("form.submit", "Register integration"),
            startIcon: <BellPlus className={ICON_CLASS} aria-hidden />,
            type: "submit",
            loading: isSubmitting,
          }}
        />
      </form>
    </Form>
  );
}
