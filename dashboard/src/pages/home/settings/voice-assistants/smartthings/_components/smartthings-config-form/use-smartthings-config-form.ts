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
import {
  useConfigureSmartThings,
  useDeleteSmartThingsConfig,
} from "@/api/integrations";
import type { SmartThingsConfigGetResponse } from "@/api/integrations";
import { normalizeApiError } from "@/lib/normalize-api-error";
import { voidFormSubmit } from "@/lib/void-form-submit";
import {
  buildSmartThingsConfigFormDefaults,
  buildSmartThingsConfigFormSchema,
  buildSmartThingsConfigPayload,
  getSmartThingsConfigFormSchemaMessages,
  type SmartThingsConfigFormValues,
} from "./smartthings-config-form.schema";

interface UseSmartThingsConfigFormOptions {
  /** Saved configuration used to seed the form; absent when configuring anew. */
  initialData?: SmartThingsConfigGetResponse;
  /** Called after a successful save or delete so the host can close itself. */
  onSuccess?: () => void;
}

export function useSmartThingsConfigForm({
  initialData,
  onSuccess,
}: UseSmartThingsConfigFormOptions) {
  const { t } = useTranslation("voice-assistants");

  const schema = useMemo(
    () =>
      buildSmartThingsConfigFormSchema(
        getSmartThingsConfigFormSchemaMessages(t),
      ),
    [t],
  );

  const form = useForm<SmartThingsConfigFormValues>({
    resolver: zodResolver(schema),
    defaultValues: useMemo(
      () => buildSmartThingsConfigFormDefaults(initialData),
      [initialData],
    ),
    mode: "onSubmit",
  });

  const configureMutation = useConfigureSmartThings();
  const deleteMutation = useDeleteSmartThingsConfig();

  const submit = voidFormSubmit(
    form.handleSubmit((values) => {
      configureMutation.mutate(buildSmartThingsConfigPayload(values), {
        onSuccess: () => {
          toast.success(
            t(
              "smartthings.form.submitSuccess",
              "SmartThings configuration saved successfully.",
            ),
          );
          onSuccess?.();
        },
      });
    }),
  );

  // Lets `mutateAsync` throw so ConfirmationDialog stays open on failure; the
  // error is surfaced inline via `deleteErrorMessage`.
  const handleDelete = async () => {
    await deleteMutation.mutateAsync();
    toast.success(
      t(
        "smartthings.form.delete.success",
        "SmartThings configuration deleted successfully.",
      ),
    );
    onSuccess?.();
  };

  const submitErrorMessage = useMemo(() => {
    if (!configureMutation.isError) {
      return undefined;
    }
    return normalizeApiError(
      configureMutation.error,
      t(
        "smartthings.form.submitError",
        "Failed to save SmartThings configuration.",
      ),
    );
  }, [configureMutation.isError, configureMutation.error, t]);

  const deleteErrorMessage = useMemo(() => {
    if (!deleteMutation.isError) {
      return undefined;
    }
    return normalizeApiError(
      deleteMutation.error,
      t(
        "smartthings.form.delete.error",
        "Failed to delete SmartThings configuration.",
      ),
    );
  }, [deleteMutation.isError, deleteMutation.error, t]);

  return {
    form,
    submit,
    isSubmitting: configureMutation.isPending || form.formState.isSubmitting,
    submitErrorMessage,
    handleDelete,
    isDeleting: deleteMutation.isPending,
    deleteErrorMessage,
    canDelete: Boolean(initialData?.client_id),
  };
}
