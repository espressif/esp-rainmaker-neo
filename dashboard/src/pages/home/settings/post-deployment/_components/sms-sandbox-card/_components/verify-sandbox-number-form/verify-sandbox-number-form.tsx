/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { Check, X } from "lucide-react";
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
import { snsErrorMessageKey } from "../../sms-sandbox-card.utils";
import {
  buildVerifySandboxNumberFormSchema,
  getVerifySandboxNumberFormSchemaMessages,
  VERIFY_SANDBOX_NUMBER_FORM_DEFAULTS,
  type VerifySandboxNumberFormValues,
} from "./verify-sandbox-number-form.schema";
import type { VerifySandboxNumberFormProps } from "./verify-sandbox-number-form.props";
import { voidFormSubmit } from "@/lib/void-form-submit";

const ICON_CLASS = "h-4 w-4 shrink-0";

/** Confirms the one-time code AWS texted to a pending destination number. */
export default function VerifySandboxNumberForm({
  onSubmit,
  onSuccess,
  onCancel,
}: VerifySandboxNumberFormProps) {
  const { t } = useTranslation(["post-deployment", "common"]);
  const [submitErrorKey, setSubmitErrorKey] = useState<string | null>(null);

  const schema = useMemo(
    () =>
      buildVerifySandboxNumberFormSchema(getVerifySandboxNumberFormSchemaMessages(t)),
    [t],
  );

  const form = useForm<VerifySandboxNumberFormValues>({
    resolver: zodResolver(schema),
    defaultValues: VERIFY_SANDBOX_NUMBER_FORM_DEFAULTS,
    mode: "onSubmit",
  });

  const submit = voidFormSubmit(form.handleSubmit(async (values) => {
    setSubmitErrorKey(null);
    try {
      await onSubmit(values.one_time_password);
      onSuccess();
    } catch (error) {
      setSubmitErrorKey(snsErrorMessageKey(error));
    }
  }),
  );

  const isSubmitting = form.formState.isSubmitting;

  return (
    <Form {...form}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-2">
        <div className="flex items-start gap-2">
          <FormField
            control={form.control}
            name="one_time_password"
            render={({ field, fieldState }) => (
              <FormItem className="w-40">
                <FormControl>
                  <Input
                    {...field}
                    size="sm"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    maxLength={6}
                    aria-label={t("smsSandbox.oneTimePasswordLabel", "One-time code")}
                    placeholder={t("smsSandbox.oneTimePasswordPlaceholder", "123456")}
                    error={!!fieldState.error}
                    disabled={isSubmitting}
                    autoFocus
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <Button
            type="submit"
            size="sm"
            fullWidth={false}
            startIcon={<Check className={ICON_CLASS} aria-hidden />}
            loading={isSubmitting}
          >
            {t("smsSandbox.verifySubmit", "Confirm")}
          </Button>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            color="gray"
            fullWidth={false}
            startIcon={<X className={ICON_CLASS} aria-hidden />}
            onClick={onCancel}
            disabled={isSubmitting}
          >
            {t("common:actions.cancel", "Cancel")}
          </Button>
        </div>

        {submitErrorKey && (
          <Alert type="error" variant="soft" size="sm" description={t(submitErrorKey)} />
        )}
      </form>
    </Form>
  );
}
