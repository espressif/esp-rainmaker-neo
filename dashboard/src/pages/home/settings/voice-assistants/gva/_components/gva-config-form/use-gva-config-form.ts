/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useSetState } from "react-use";
import { useTranslation } from "react-i18next";
import { toast } from "@espressif/dashboard-ui-components/components";
import { useConfigureGva, useDeleteGvaConfig } from "@/api/integrations";
import type { GvaConfigGetResponse } from "@/api/integrations";
import { normalizeApiError } from "@/lib/normalize-api-error";
import {
  buildGvaConfigFormDefaults,
  buildGvaConfigFormSchema,
  buildGvaConfigPayload,
  getGvaConfigFormSchemaMessages,
  type GvaConfigFormValues,
} from "./gva-config-form.schema";
import { voidFormSubmit } from "@/lib/void-form-submit";

export type GvaEditMethod = "upload" | "manual";

interface UseGvaConfigFormOptions {
  /** Saved configuration used to seed the form; absent when configuring anew. */
  initialData?: GvaConfigGetResponse;
  /** Called after a successful save or delete so the host can close itself. */
  onSuccess?: () => void;
}

interface GvaConfigFormViewState {
  editMethod: GvaEditMethod;
  fileName: string | null;
  fileError: string | null;
}

const INITIAL_VIEW_STATE: GvaConfigFormViewState = {
  editMethod: "upload",
  fileName: null,
  fileError: null,
};

/**
 * Owns the GVA configuration form: schema, configure/delete mutations, the
 * upload-vs-manual view state and the derived loading/error messages. The
 * consuming component stays a thin, container-agnostic view. The same POST
 * endpoint handles both first-time configuration and edits.
 */
export function useGvaConfigForm({
  initialData,
  onSuccess,
}: UseGvaConfigFormOptions) {
  const { t } = useTranslation("voice-assistants");

  const schema = useMemo(
    () => buildGvaConfigFormSchema(getGvaConfigFormSchemaMessages(t)),
    [t],
  );

  const form = useForm<GvaConfigFormValues>({
    resolver: zodResolver(schema),
    defaultValues: useMemo(
      () => buildGvaConfigFormDefaults(initialData),
      [initialData],
    ),
    mode: "onSubmit",
  });

  const [view, setView] = useSetState<GvaConfigFormViewState>(INITIAL_VIEW_STATE);

  const configureMutation = useConfigureGva();
  const deleteMutation = useDeleteGvaConfig();

  const setEditMethod = (editMethod: GvaEditMethod) => {
    setView({ editMethod });
  };

  /**
   * Read, parse and validate an uploaded service-account JSON, then populate the
   * form. Only the three core fields must be present (see schema); anything else
   * is optional. Mirrors the legacy `populateFromJson` flow.
   */
  const handleFileChange = async (files: File[]) => {
    const file = files[0];
    if (!file) {
      return;
    }
    setView({ fileName: null, fileError: null });
    configureMutation.reset();
    try {
      const parsed = JSON.parse(await file.text()) as unknown;
      const result = schema.safeParse(parsed);
      if (!result.success) {
        setView({ fileError: t("gva.invalidJsonFormat", "The JSON file does not contain the required service account fields (project_id, client_email, private_key)") });
        return;
      }
      form.reset({ ...buildGvaConfigFormDefaults(), ...result.data });
      setView({ fileName: file.name, fileError: null });
    } catch {
      setView({ fileError: t("gva.invalidJsonFile", "The selected file is not valid JSON") });
    }
  };

  const submit = voidFormSubmit(form.handleSubmit(
    (values) => {
      configureMutation.mutate(buildGvaConfigPayload(values), {
        onSuccess: () => {
          toast.success(
            t("gva.form.submitSuccess", "GVA configuration saved successfully."),
          );
          onSuccess?.();
        },
      });
    },
    () => {
      // Validation errors on hidden upload-mode fields are invisible; reveal the
      // manual fields so the user can see and fix them.
      if (view.editMethod === "upload") {
        setView({ editMethod: "manual" });
      }
    },
  ),
  );

  // Lets `mutateAsync` throw so ConfirmationDialog stays open on failure; the
  // error is surfaced inline via `deleteErrorMessage`.
  const handleDelete = async () => {
    await deleteMutation.mutateAsync();
    toast.success(
      t("gva.form.delete.success", "GVA configuration deleted successfully."),
    );
    onSuccess?.();
  };

  const submitErrorMessage = useMemo(() => {
    if (!configureMutation.isError) {
      return undefined;
    }
    return normalizeApiError(
      configureMutation.error,
      t("gva.form.submitError", "Failed to save GVA configuration."),
    );
  }, [configureMutation.isError, configureMutation.error, t]);

  const deleteErrorMessage = useMemo(() => {
    if (!deleteMutation.isError) {
      return undefined;
    }
    return normalizeApiError(
      deleteMutation.error,
      t("gva.form.delete.error", "Failed to delete GVA configuration."),
    );
  }, [deleteMutation.isError, deleteMutation.error, t]);

  return {
    form,
    submit,
    isSubmitting: configureMutation.isPending || form.formState.isSubmitting,
    submitErrorMessage,
    editMethod: view.editMethod,
    setEditMethod,
    fileName: view.fileName,
    fileError: view.fileError,
    handleFileChange,
    handleDelete,
    isDeleting: deleteMutation.isPending,
    deleteErrorMessage,
    canDelete: Boolean(initialData?.project_id),
  };
}
