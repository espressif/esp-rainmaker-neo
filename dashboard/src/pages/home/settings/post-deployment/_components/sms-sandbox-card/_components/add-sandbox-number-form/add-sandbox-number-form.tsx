/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { MessageSquarePlus, Plus } from "lucide-react";
import {
  Alert,
  Button,
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import { snsErrorMessageKey } from "../../sms-sandbox-card.utils";
import {
  ADD_SANDBOX_NUMBER_FORM_DEFAULTS,
  buildAddSandboxNumberFormSchema,
  getAddSandboxNumberFormSchemaMessages,
  type AddSandboxNumberFormValues,
} from "./add-sandbox-number-form.schema";
import type { AddSandboxNumberFormProps } from "./add-sandbox-number-form.props";
import { voidFormSubmit } from "@/lib/void-form-submit";

/** Registers a destination number with the SMS sandbox; AWS then texts it a one-time code. */
export default function AddSandboxNumberForm({
  onSubmit,
  onSuccess,
  disabled,
}: AddSandboxNumberFormProps) {
  const { t } = useTranslation(["post-deployment", "common"]);
  const [submitErrorKey, setSubmitErrorKey] = useState<string | null>(null);

  const schema = useMemo(
    () => buildAddSandboxNumberFormSchema(getAddSandboxNumberFormSchemaMessages(t)),
    [t],
  );

  const form = useForm<AddSandboxNumberFormValues>({
    resolver: zodResolver(schema),
    defaultValues: ADD_SANDBOX_NUMBER_FORM_DEFAULTS,
    mode: "onSubmit",
  });

  const submit = voidFormSubmit(form.handleSubmit(async (values) => {
    setSubmitErrorKey(null);
    try {
      await onSubmit(values.phone_number);
      form.reset(ADD_SANDBOX_NUMBER_FORM_DEFAULTS);
      onSuccess();
    } catch (error) {
      setSubmitErrorKey(snsErrorMessageKey(error));
    }
  }),
  );

  const isSubmitting = form.formState.isSubmitting;
  const addLabel = t("smsSandbox.phoneNumberLabel", "Add a destination number");

  return (
    <SectionCard
      size="sm"
      variant="soft"
      color="mist"
      icon={<MessageSquarePlus className="h-4 w-4" />}
      primaryText={addLabel}
      secondaryText={t(
        "smsSandbox.phoneNumberHint",
        "Use E.164 format, for example +15551234567",
      )}
    >
      <Form {...form}>
        <form noValidate onSubmit={submit} className="flex flex-col gap-3">
          <FormField
            control={form.control}
            name="phone_number"
            render={({ field, fieldState }) => (
              <FormItem>
                <FormControl>
                  <Input
                    {...field}
                    type="tel"
                    inputMode="tel"
                    autoComplete="off"
                    // The section card already carries the label, so repeating it above
                    // the field would just duplicate it.
                    aria-label={addLabel}
                    placeholder={t("smsSandbox.phoneNumberPlaceholder", "+15551234567")}
                    error={!!fieldState.error}
                    disabled={disabled || isSubmitting}
                    endAddOnContent={
                      <Button
                        type="submit"
                        size="sm"
                        variant="ghost"
                        fullWidth={false}
                        startIcon={<Plus className="h-4 w-4" aria-hidden />}
                        loading={isSubmitting}
                        disabled={disabled}
                      >
                        {t("common:actions.add", "Add")}
                      </Button>
                    }
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          {submitErrorKey && (
            <Alert
              hideIcon
              type="error"
              variant="soft"
              description={t(submitErrorKey)}
            />
          )}
        </form>
      </Form>
    </SectionCard>
  );
}
