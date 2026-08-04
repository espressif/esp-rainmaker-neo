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
} from "@espressif/dashboard-ui-components/components";
import {
  getForgotPasswordRequestSchema,
  getAuthSchemaMessages,
  useForgotPassword,
  type ForgotPasswordRequestSchema,
} from "@/api";
import { requestCodeErrorMessage } from "@/lib/auth";
import type { RequestCodeFormProps } from "./request-code-form.props";
import { voidFormSubmit } from "@/lib/void-form-submit";

/**
 * Step 1 of the reset flow: ask Cognito to mail a confirmation code.
 */
export default function RequestCodeForm({
  onCodeSent,
  onHasCode,
}: RequestCodeFormProps) {
  const { t } = useTranslation("forgot-password");
  const schema = useMemo(() => getForgotPasswordRequestSchema(getAuthSchemaMessages(t)), [t]);
  const requestMutation = useForgotPassword();

  const form = useForm<ForgotPasswordRequestSchema>({
    resolver: zodResolver(schema),
    defaultValues: { username: "" },
  });

  const requestCode = ({ username }: ForgotPasswordRequestSchema) => {
    requestMutation.mutate(
      { username },
      { onSuccess: () => onCodeSent(username) },
    );
  };

  const handleHasCode = async () => {
    if (await form.trigger("username")) {
      onHasCode(form.getValues("username"));
    }
  };

  const errorMessage = requestCodeErrorMessage(requestMutation.error);

  return (
    <Form {...form}>
      <form onSubmit={voidFormSubmit(form.handleSubmit(requestCode))} className="space-y-6">
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
          name="username"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <Input
                  type="email"
                  autoComplete="username"
                  placeholder={t(
                    "emailPlaceholder",
                    "Enter your email address",
                  )}
                  label={t("emailLabel", "Email")}
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
            loading={requestMutation.isPending}
            size="lg"
            endIcon={<ArrowRightIcon className="w-4 h-4" />}
            animateEndIconOnHover={true}
            loadingIndicator="progress-bar"
          >
            {t("sendCode", "Send code")}
          </Button>
          <Button
            type="button"
            variant="outline"
            animateEndIconOnHover={true}
            onClick={() => void handleHasCode()}
            size="lg"
          >
            {t("haveCode", "I already have a code")}
          </Button>
        </div>
      </form>
    </Form>
  );
}
