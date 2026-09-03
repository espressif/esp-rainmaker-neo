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
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
} from "@espressif/dashboard-ui-components/components";
import {
  getAuthSchemaMessages,
  getIdentifyRequestSchema,
  type IdentifyRequestSchema,
} from "@/api";
import { voidFormSubmit } from "@/lib/void-form-submit";
import type { EmailFormProps } from "./email-form.props";

/** Screen 3's form: collect the address, so Cognito can say which factors it has. */
export default function EmailForm({
  defaultUsername,
  isSubmitting,
  onSubmit,
}: EmailFormProps) {
  const { t } = useTranslation(["login", "common"]);
  const schema = useMemo(
    () => getIdentifyRequestSchema(getAuthSchemaMessages(t)),
    [t],
  );
  const form = useForm<IdentifyRequestSchema>({
    resolver: zodResolver(schema),
    defaultValues: { username: defaultUsername },
  });

  return (
    <Form {...form}>
      <form
        onSubmit={voidFormSubmit(
          form.handleSubmit((data) => onSubmit(data.username)),
        )}
        className="space-y-6"
      >
        <FormField
          control={form.control}
          name="username"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <Input
                  type="email"
                  autoComplete="username"
                  label={t("emailLabel", "Email")}
                  placeholder={t("emailPlaceholder", "Enter your email")}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <Button
          type="submit"
          loading={isSubmitting}
          size="lg"
          endIcon={<ArrowRightIcon className="w-4 h-4" />}
          animateEndIconOnHover={true}
          loadingIndicator="progress-bar"
        >
          {t("continue", "Continue")}
        </Button>
      </form>
    </Form>
  );
}
