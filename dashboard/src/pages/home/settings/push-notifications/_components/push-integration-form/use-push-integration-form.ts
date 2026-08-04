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
import { useRegisterPushIntegration } from "@/api/integrations";
import { normalizeApiError } from "@/lib/normalize-api-error";
import {
  buildPushIntegrationFormSchema,
  getPushIntegrationFormSchemaMessages,
  PUSH_INTEGRATION_FORM_DEFAULTS,
  type PushIntegrationFormValues,
} from "./push-integration-form.schema";
import {
  buildIosRegisterPayload,
  parseServiceAccountFile,
  ServiceAccountFileError,
  type PushIntegrationRegisterPayload,
} from "./push-integration-form.utils";
import { voidFormSubmit } from "@/lib/void-form-submit";

interface UsePushIntegrationFormOptions {
  /** Called after a successful registration so the host can close itself. */
  onSuccess?: () => void;
}

/**
 * Owns the "Add push notification integration" form: schema, mutation, submit
 * mapping, and derived loading/error state. The component consuming this stays
 * a thin, container-agnostic view.
 */
export function usePushIntegrationForm({
  onSuccess,
}: UsePushIntegrationFormOptions) {
  const { t } = useTranslation("push-notifications");

  const schema = useMemo(
    () => buildPushIntegrationFormSchema(getPushIntegrationFormSchemaMessages(t)),
    [t],
  );

  const form = useForm<PushIntegrationFormValues>({
    resolver: zodResolver(schema),
    defaultValues: PUSH_INTEGRATION_FORM_DEFAULTS,
    mode: "onSubmit",
  });

  const registerMutation = useRegisterPushIntegration();
  const integrationType = form.watch("integration_type");

  /**
   * Build the register payload. For Android the uploaded JSON is parsed here;
   * a parse/format failure sets a field-level error and returns `null` so the
   * mutation is never fired.
   */
  const resolvePayload = async (
    values: PushIntegrationFormValues,
  ): Promise<PushIntegrationRegisterPayload | null> => {
    if (values.integration_type === "ios") {
      return buildIosRegisterPayload(values);
    }

    try {
      const serviceAccount = await parseServiceAccountFile(
        values.service_account[0],
      );
      return { integrationType: "gcm", data: serviceAccount };
    } catch (error) {
      const messageKey =
        error instanceof ServiceAccountFileError &&
        error.reason === "invalid-format"
          ? "invalidJsonFormat"
          : "invalidJsonFile";
      form.setError("service_account", { message: t(messageKey) });
      return null;
    }
  };

  const submit = voidFormSubmit(form.handleSubmit(async (values) => {
    const payload = await resolvePayload(values);
    if (!payload) {
      return;
    }

    registerMutation.mutate(payload, {
      onSuccess: () => {
        toast.success(t("registerSuccess", "Push integration registered successfully"));
        onSuccess?.();
      },
    });
  }),
  );

  const submitErrorMessage = useMemo(() => {
    if (!registerMutation.isError) {
      return undefined;
    }
    return normalizeApiError(registerMutation.error, t("registerError", "Failed to register push integration"));
  }, [registerMutation.isError, registerMutation.error, t]);

  return {
    form,
    integrationType,
    submit,
    isSubmitting: registerMutation.isPending || form.formState.isSubmitting,
    submitErrorMessage,
  };
}
