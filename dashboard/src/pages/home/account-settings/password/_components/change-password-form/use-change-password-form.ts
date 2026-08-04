/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import {
  getChangePasswordRequestSchema,
  getAuthSchemaMessages,
  useChangePassword,
  type ChangePasswordRequestSchema,
} from "@/api";
import {
  changePasswordErrorMessage,
  getAccessToken,
  isIncorrectCurrentPasswordError,
  SESSION_EXPIRED_MESSAGE,
  type LocalizedMessage,
} from "@/lib/auth";
import { evaluatePasswordPolicy } from "@/config/password-policy.config";
import type { RequirementListItem } from "@espressif/dashboard-ui-components/components";
import { voidFormSubmit } from "@/lib/void-form-submit";

const EMPTY_VALUES: ChangePasswordRequestSchema = {
  old_password: "",
  new_password: "",
  confirm_password: "",
};

interface UseChangePasswordFormOptions {
  onSuccess: () => void;
}

/**
 * Owns everything the change-password form needs: validation, the Cognito mutation,
 * the translated requirements checklist, and failure messages.
 *
 * `mode: "onTouched"` gives feedback as soon as a field is left rather than only on
 * submit — with three password fields, waiting until submit means fixing all of them
 * at once.
 */
export function useChangePasswordForm({
  onSuccess,
}: UseChangePasswordFormOptions) {
  const { t } = useTranslation("common");
  const schema = useMemo(() => getChangePasswordRequestSchema(getAuthSchemaMessages(t)), [t]);
  const changePasswordMutation = useChangePassword();

  /**
   * Set when the request cannot even be attempted (no access token). Kept apart from
   * the mutation's own error rather than swallowed, so the admin is told why nothing
   * happened.
   */
  const [preflightError, setPreflightError] = useState<LocalizedMessage | null>(
    null,
  );

  const form = useForm<ChangePasswordRequestSchema>({
    resolver: zodResolver(schema),
    defaultValues: EMPTY_VALUES,
    mode: "onTouched",
  });

  const submit = voidFormSubmit(form.handleSubmit((values) => {
    const accessToken = getAccessToken();
    if (!accessToken) {
      setPreflightError(SESSION_EXPIRED_MESSAGE);
      return;
    }

    setPreflightError(null);
    changePasswordMutation.mutate(
      {
        access_token: accessToken,
        old_password: values.old_password,
        new_password: values.new_password,
      },
      {
        onSuccess: () => {
          form.reset(EMPTY_VALUES);
          onSuccess();
        },
        onError: (error) => {
          // Wrong current password is a field problem, not a form-wide one — put the
          // message where the admin has to type, and let the alert repeat it.
          if (isIncorrectCurrentPasswordError(error)) {
            form.setError("old_password", {
              type: "server",
              message: t(
                "passwordErrors.incorrectCurrentPassword",
                "Your current password is not correct. Try again.",
              ),
            });
          }
        },
      },
    );
  }),
  );

  const submitErrorMessage = useMemo(
    () => preflightError ?? changePasswordErrorMessage(changePasswordMutation.error),
    [preflightError, changePasswordMutation.error],
  );

  const newPassword = form.watch("new_password");
  const requirementItems = useMemo<RequirementListItem[]>(
    () =>
      evaluatePasswordPolicy(newPassword).map(({ rule, met }) => ({
        id: rule.id,
        label: t(rule.i18nKey, rule.fallback),
        met,
      })),
    [newPassword, t],
  );

  return {
    form,
    submit,
    requirementItems,
    submitErrorMessage,
    isSubmitting:
      changePasswordMutation.isPending || form.formState.isSubmitting,
  };
}
