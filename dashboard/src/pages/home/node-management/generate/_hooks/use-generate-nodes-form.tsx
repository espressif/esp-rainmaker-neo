/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { getIotEndpoint } from "@/lib/config";
import {
  buildGenerateNodesFormSchema,
  getGenerateNodesFormSchemaMessages,
  type GenerateNodesFormValues,
} from "../_schema/generate-nodes-form.schema";
import { useGenerateOrchestration } from "./use-generate-orchestration";

const DEFAULT_VALUES: GenerateNodesFormValues = {
  count: 5,
  matter: false,
};

export function useGenerateNodesForm() {
  const { t } = useTranslation("nodes");
  const orchestration = useGenerateOrchestration();

  const schema = useMemo(
    () => buildGenerateNodesFormSchema(getGenerateNodesFormSchemaMessages(t)),
    [t],
  );

  // Generated devices embed the IoT endpoint in their NVS partition, so block
  // the flow up front (with an inline notice) when the deployment lacks one.
  const isIotEndpointConfigured = useMemo(
    () => Boolean(getIotEndpoint()),
    [],
  );

  const form = useForm<GenerateNodesFormValues>({
    resolver: zodResolver(schema),
    defaultValues: DEFAULT_VALUES,
    mode: "onSubmit",
  });

  const handleSubmit = useCallback(
    (values: GenerateNodesFormValues) => {
      void orchestration.startFlow(values);
    },
    [orchestration],
  );

  // Closing the dialog (via its X) is now the "start over" path, so reset the
  // form back to defaults alongside the orchestration state.
  const handleDialogOpenChange = useCallback(
    (open: boolean) => {
      orchestration.handleDialogOpenChange(open);
      if (!open) {
        form.reset(DEFAULT_VALUES);
      }
    },
    [orchestration, form],
  );

  const retry = useCallback(() => {
    void orchestration.startFlow(form.getValues());
  }, [orchestration, form]);

  return {
    t,
    form,
    status: orchestration.status,
    dialogOpen: orchestration.dialogOpen,
    errorMessage: orchestration.errorMessage,
    downloaded: orchestration.downloaded,
    isGenerating: orchestration.status === "generating",
    isIotEndpointConfigured,
    handleSubmit,
    handleDialogOpenChange,
    download: orchestration.download,
    registerNodes: orchestration.registerNodes,
    retry,
  };
}
