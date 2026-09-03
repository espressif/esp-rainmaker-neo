/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowRightIcon } from "lucide-react";
import {
  Alert,
  Button,
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  InputOTP,
  InputPassword,
  RequirementList,
  SectionCard,
  type RequirementListItem,
} from "@espressif/dashboard-ui-components/components";
import { ResendCodeHint } from "@/components/resend-code-hint";
import { evaluatePasswordPolicy } from "@/config/password-policy.config";
import {
  getConfirmForgotPasswordRequestSchema,
  getAuthSchemaMessages,
  useConfirmForgotPassword,
  useForgotPassword,
  type ConfirmForgotPasswordRequestSchema,
} from "@/api";
import { confirmResetErrorMessage, requestCodeErrorMessage } from "@/lib/auth";
import { useResendCooldown } from "@/hooks/use-resend-cooldown";
import type { SetNewPasswordFormProps } from "./set-new-password-form.props";
import { voidFormSubmit } from "@/lib/void-form-submit";

const CODE_LENGTH = 6;

/**
 * Step 2 of the reset flow: exchange the emailed code for a new password.
 * The password rules come from the shared schema, so they match the ones the
 * change-password page enforces.
 */
export default function SetNewPasswordForm({
  email,
  codeJustSent,
  onSuccess,
  onCodeResent,
}: SetNewPasswordFormProps) {
  const { t } = useTranslation("set-password");
  const schema = useMemo(() => getConfirmForgotPasswordRequestSchema(getAuthSchemaMessages(t)), [t]);
  const confirmMutation = useConfirmForgotPassword();
  const resendMutation = useForgotPassword();
  const cooldown = useResendCooldown(codeJustSent);

  const form = useForm<ConfirmForgotPasswordRequestSchema>({
    resolver: zodResolver(schema),
    defaultValues: { code: "", new_password: "" },
  });

  const resetPassword = (data: ConfirmForgotPasswordRequestSchema) => {
    resendMutation.reset();
    confirmMutation.mutate(
      { username: email, code: data.code, new_password: data.new_password },
      { onSuccess },
    );
  };

  const resendCode = () => {
    confirmMutation.reset();
    resendMutation.mutate(
      { username: email },
      {
        onSuccess: () => {
          cooldown.restart();
          onCodeResent();
        },
      },
    );
  };

  // A failed reset is what the admin is looking at right now, so it wins over
  // a stale resend failure.
  const errorMessage =
    confirmResetErrorMessage(confirmMutation.error) ??
    requestCodeErrorMessage(resendMutation.error);

  // Live policy checklist, same as the change-password form: it renders in place
  // of the password field's `FormMessage`, which would repeat the failing rule.
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

  const resendControl = (
    <ResendCodeHint
      isCoolingDown={cooldown.isCoolingDown}
      countdownLabel={t("resendCodeIn", {
        defaultValue: "Resend code in {{seconds}}s",
        seconds: cooldown.secondsLeft,
      })}
      resendLabel={t("resendCode", "Resend code")}
      isResending={resendMutation.isPending}
      onResend={resendCode}
    />
  );

  return (
    <Form {...form}>
      <form onSubmit={voidFormSubmit(form.handleSubmit(resetPassword))} className="space-y-6">
        {errorMessage && (
          <Alert
            title={t("errorTitle", "Unable to reset password")}
            type="error"
            description={t(errorMessage.key, errorMessage.fallback)}
            hideIcon
            className="border-none shadow-none"
          />
        )}

        <FormField
          control={form.control}
          name="code"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <InputOTP
                  length={CODE_LENGTH}
                  autoComplete="one-time-code"
                  label={t("codeLabel", "Verification code")}
                  value={field.value}
                  onChange={field.onChange}
                  expand
                  hintContent={resendControl}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="new_password"
          render={({ field, fieldState }) => (
            <FormItem>
              <FormControl>
                <InputPassword
                  autoComplete="new-password"
                  placeholder={t("newPasswordPlaceholder", "New password")}
                  label={t("newPasswordLabel", "New password")}
                  error={!!fieldState.error}
                  {...field}
                />
              </FormControl>
              <SectionCard
                className="mt-4"
                primaryText={t("requirementsLabel", "Password requirements")}
                allowCollapse={false}
                color="silver"
                variant="soft"
                size="sm"
              >
                <RequirementList
                  items={requirementItems}
                  metLabel={t("requirementMet", "Requirement met")}
                  unmetLabel={t("requirementUnmet", "Requirement not met")}
                />
              </SectionCard>
            </FormItem>
          )}
        />

        <div className="flex flex-wrap items-center gap-4">
          <Button
            type="submit"
            loading={confirmMutation.isPending}
            size="lg"
            endIcon={<ArrowRightIcon className="w-4 h-4" />}
            animateEndIconOnHover={true}
            loadingIndicator="progress-bar"
          >
            {t("submit", "Reset password")}
          </Button>
        </div>
      </form>
    </Form>
  );
}
