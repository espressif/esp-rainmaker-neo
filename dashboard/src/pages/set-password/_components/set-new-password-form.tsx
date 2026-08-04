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
  Input,
  InputPassword,
  Link,
} from "@espressif/dashboard-ui-components/components";
import {
  getConfirmForgotPasswordRequestSchema,
  getAuthSchemaMessages,
  useConfirmForgotPassword,
  useForgotPassword,
  type ConfirmForgotPasswordRequestSchema,
} from "@/api";
import { confirmResetErrorMessage, requestCodeErrorMessage } from "@/lib/auth";
import type { SetNewPasswordFormProps } from "./set-new-password-form.props";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import { voidFormSubmit } from "@/lib/void-form-submit";

/**
 * Step 2 of the reset flow: exchange the emailed code for a new password.
 * The password rules come from the shared schema, so they match the ones the
 * change-password page enforces.
 */
export default function SetNewPasswordForm({
  email,
  onSuccess,
  onCodeResent,
}: SetNewPasswordFormProps) {
  const { t } = useTranslation("set-password");
  const schema = useMemo(() => getConfirmForgotPasswordRequestSchema(getAuthSchemaMessages(t)), [t]);
  const confirmMutation = useConfirmForgotPassword();
  const resendMutation = useForgotPassword();

  const form = useForm<ConfirmForgotPasswordRequestSchema>({
    resolver: zodResolver(schema),
    defaultValues: { code: "", new_password: "", confirm_password: "" },
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
    resendMutation.mutate({ username: email }, { onSuccess: onCodeResent });
  };

  // A failed reset is what the admin is looking at right now, so it wins over
  // a stale resend failure.
  const errorMessage =
    confirmResetErrorMessage(confirmMutation.error) ??
    requestCodeErrorMessage(resendMutation.error);

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
                <Input
                  type="text"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  maxLength={6}
                  placeholder={t("codePlaceholder", "6-digit code")}
                  label={t("codeLabel", "Confirmation code")}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="new_password"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <InputPassword
                  autoComplete="new-password"
                  placeholder={t("newPasswordPlaceholder", "New password")}
                  label={t("newPasswordLabel", "New Password")}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="confirm_password"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <InputPassword
                  autoComplete="new-password"
                  placeholder={t(
                    "confirmPasswordPlaceholder",
                    "Confirm new password",
                  )}
                  label={t("confirmPasswordLabel", "Confirm Password")}
                  {...field}
                />
              </FormControl>
              <FormMessage />
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
          <Button
            type="button"
            variant="outline"
            loading={resendMutation.isPending}
            onClick={resendCode}
          >
            {t("resendCode", "Resend code")}
          </Button>
          <Link
            to="/forgot-password"
            linkComponent={TanstackRouterLink}
            color="primary"
            underline={false}
            className="w-full flex items-center justify-center"
          >
            {t("useDifferentEmail", "Use a different email")}
          </Link>
        </div>
      </form>
    </Form>
  );
}
