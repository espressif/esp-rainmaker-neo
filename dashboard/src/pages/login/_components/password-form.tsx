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
  Input,
  InputPassword,
  Label,
  Link,
} from "@espressif/dashboard-ui-components/components";
import {
  getAuthSchemaMessages,
  getSigninRequestSchema,
  type SigninRequestSchema,
} from "@/api";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import { voidFormSubmit } from "@/lib/void-form-submit";

interface PasswordFormProps {
  defaultUsername: string;
  allowKeepMeSignedIn: boolean;
  keepSignedIn: boolean;
  onKeepSignedInChange: (checked: boolean) => void;
  isSubmitting: boolean;
  onSubmit: (data: SigninRequestSchema) => void;
}

/** Password fallback: the one path that works whether or not the admin has an email factor. */
export default function PasswordForm({
  defaultUsername,
  allowKeepMeSignedIn,
  keepSignedIn,
  onKeepSignedInChange,
  isSubmitting,
  onSubmit,
}: PasswordFormProps) {
  const { t } = useTranslation(["login", "common"]);
  const schema = useMemo(() => getSigninRequestSchema(getAuthSchemaMessages(t)), [t]);

  const form = useForm<SigninRequestSchema>({
    resolver: zodResolver(schema),
    defaultValues: {
      username: defaultUsername,
      password: "",
    },
  });

  return (
    <Form {...form}>
      <form onSubmit={voidFormSubmit(form.handleSubmit(onSubmit))} className="space-y-6">
        <FormField
          control={form.control}
          name="username"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <Input
                  type="text"
                  placeholder={t("emailPlaceholder", "Enter your email")}
                  label={t("emailLabel", "Email")}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="password"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <InputPassword
                  placeholder={t(
                    "passwordPlaceholder",
                    "Enter your password",
                  )}
                  autoComplete="current-password"
                  label={t("passwordLabel", "Password")}
                  hintContent={
                    <Link
                      to="/forgot-password"
                      linkComponent={TanstackRouterLink}
                      color="primary"
                      underline={false}
                    >
                      {t("forgotPasswordLink", "Forgot password?")}
                    </Link>
                  }
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
