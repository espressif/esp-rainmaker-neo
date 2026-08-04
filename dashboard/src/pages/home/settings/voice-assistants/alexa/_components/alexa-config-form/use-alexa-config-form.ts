/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { toast } from "@espressif/dashboard-ui-components/components";
import { useConfigureAlexa } from "@/api/integrations";
import type { AlexaConfigGetResponse } from "@/api/integrations";
import { normalizeApiError } from "@/lib/normalize-api-error";
import {
  buildAlexaConfigFormDefaults,
  buildAlexaConfigFormSchema,
  getAlexaConfigFormSchemaMessages,
  type AlexaConfigFormValues,
} from "./alexa-config-form.schema";
import { voidFormSubmit } from "@/lib/void-form-submit";

interface UseAlexaConfigFormOptions {

  initialData?: AlexaConfigGetResponse;

  onSuccess?: () => void;
}

export function useAlexaConfigForm({
  initialData,
  onSuccess,
}: UseAlexaConfigFormOptions) {
  const { t } = useTranslation("voice-assistants");

  const schema = useMemo(
    () => buildAlexaConfigFormSchema(getAlexaConfigFormSchemaMessages(t)),
    [t],
  );

  const form = useForm<AlexaConfigFormValues>({
    resolver: zodResolver(schema),
    defaultValues: useMemo(
      () => buildAlexaConfigFormDefaults(initialData),
      [initialData],
    ),
    mode: "onSubmit",
  });

  const configureMutation = useConfigureAlexa();

  const submit = voidFormSubmit(form.handleSubmit((values) => {
    configureMutation.mutate(
      {
        client_id: values.client_id,
        client_secret: values.client_secret,
        skill_id: values.skill_id,
        manufacturer_name: values.manufacturer_name,
        redirect_uris: values.redirect_uris.map((uri) => uri.value),
      },
      {
        onSuccess: () => {
          toast.success(
            t("alexa.form.submitSuccess", "Alexa configuration saved successfully."),
          );
          onSuccess?.();
        },
      },
    );
  }),
  );

  const submitErrorMessage = useMemo(() => {
    if (!configureMutation.isError) {
      return undefined;
    }
    return normalizeApiError(
      configureMutation.error,
      t("alexa.form.submitError", "Failed to save Alexa configuration."),
    );
  }, [configureMutation.isError, configureMutation.error, t]);

  return {
    form,
    submit,
    isSubmitting: configureMutation.isPending || form.formState.isSubmitting,
    submitErrorMessage,
  };
}
