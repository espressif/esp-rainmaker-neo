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
  Button,
  Checkbox,
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  InputPassword,
  Label,
} from "@espressif/dashboard-ui-components/components";
import { getAuthSchemaMessages, getSigninRequestSchema } from "@/api";
import { voidFormSubmit } from "@/lib/void-form-submit";
import type { PasswordFormProps } from "./password-form.props";

// The shared signin schema also carries `username`; here the address is fixed
// (shown as the account chip), so the form validates only the password field.
interface PasswordFormValues {
  password: string;
}

/** Screen 4's form: the password path, for admins that have one. */
export default function PasswordForm({
  allowKeepMeSignedIn,
  keepSignedIn,
  onKeepSignedInChange,
  isSubmitting,
  isRequestingReset,
  onSubmit,
  onForgotPassword,
}: PasswordFormProps) {
  const { t } = useTranslation(["login", "common"]);
  const schema = useMemo(
    () => getSigninRequestSchema(getAuthSchemaMessages(t)).pick({ password: true }),
    [t],
  );

  const form = useForm<PasswordFormValues>({
    resolver: zodResolver(schema),
    defaultValues: { password: "" },
  });

  const forgotPasswordControl = (
    <Button
      type="button"
      variant="link"
      className="px-0 h-auto"
      loading={isRequestingReset}
      onClick={onForgotPassword}
    >
      {t("forgotPasswordLink", "Forgot password?")}
    </Button>
  );

  return (
    <Form {...form}>
      <form
        onSubmit={voidFormSubmit(
          form.handleSubmit((data) => onSubmit(data.password)),
        )}
        className="space-y-6"
      >
        <FormField
          control={form.control}
          name="password"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <InputPassword
                  placeholder={t("passwordPlaceholder", "Enter your password")}
                  autoComplete="current-password"
                  autoFocus
                  label={t("passwordLabel", "Password")}
                  hintContent={forgotPasswordControl}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {allowKeepMeSignedIn && (
          <div className="flex items-center space-x-2">
            <Checkbox
              id="keep-signed-in"
              checked={keepSignedIn}
              onCheckedChange={(checked) =>
                onKeepSignedInChange(checked === true)
              }
            />
            <Label
              htmlFor="keep-signed-in"
              className="text-sm font-normal cursor-pointer"
            >
              {t("keepSignedInLabel", "Keep me signed in")}
            </Label>
          </div>
        )}

        <Button
          type="submit"
          loading={isSubmitting}
          size="lg"
          endIcon={<ArrowRightIcon className="w-4 h-4" />}
          animateEndIconOnHover={true}
          loadingIndicator="progress-bar"
        >
          {t("submit", "Sign in")}
        </Button>
      </form>
    </Form>
  );
}
