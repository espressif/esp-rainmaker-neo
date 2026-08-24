/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { KeyRound } from "lucide-react";
import { Alert, Form } from "@espressif/dashboard-ui-components/components";
import { FormFooterActions } from "@/components/form-footer-actions";
import { ChangePasswordFields } from "./_components/change-password-fields";
import { useChangePasswordForm } from "./use-change-password-form";
import type { ChangePasswordFormProps } from "./change-password-form.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

/**
 * Self-contained change-password form. Owns its schema, state, mutation, error alert
 * and footer actions, and knows nothing about the surface hosting it — the account
 * settings page renders it inline in a card, but it drops into a sheet or dialog
 * unchanged.
 */
export default function ChangePasswordForm({
  onSuccess,
}: ChangePasswordFormProps) {
  const { t } = useTranslation("account-settings");
  const { form, submit, mode, requirementItems, submitErrorMessage, isSubmitting } =
    useChangePasswordForm({ onSuccess });

  return (
    <Form {...form}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-6">
        {submitErrorMessage ? (
          <Alert
            type="error"
            variant="soft"
            color="error"
            title={t("password.errorTitle", "Could not change your password")}
            description={t(
              submitErrorMessage.key,
              submitErrorMessage.fallback,
            )}
          />
        ) : null}

        <ChangePasswordFields mode={mode} requirementItems={requirementItems} />

        <FormFooterActions
          primaryAction={{
            label:
              mode === "set"
                ? t("password.setPasswordSubmit", "Set password")
                : t("password.submit", "Update password"),
            startIcon: <KeyRound className={ICON_CLASS} aria-hidden />,
            type: "submit",
            loading: isSubmitting,
          }}
        />
      </form>
    </Form>
  );
}
