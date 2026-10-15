/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import {
  buildUpdateNodesFormSchema,
  getUpdateNodesFormSchemaMessages,
  type UpdateNodesFormValues,
} from "../_schema/update-nodes-form.schema";
import { getUpdateNodesSections } from "../_config/sections.config";
import { mapFormValuesToMutationParams } from "../_components/update-nodes-form/update-nodes-form.utils";
import { useUpdateOrchestration } from "./use-update-orchestration";

const DEFAULT_VALUES: UpdateNodesFormValues = {
  certificateFiles: [],
  groupName: undefined,
  subgroupName: undefined,
  tags: [],
};

export function useUpdateNodesForm(initialCertificateFile?: File) {
  const { t } = useTranslation("update");
  const navigate = useNavigate();
  const orchestration = useUpdateOrchestration();

  const schema = useMemo(
    () => buildUpdateNodesFormSchema(getUpdateNodesFormSchemaMessages(t)),
    [t],
  );

  const form = useForm<UpdateNodesFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      ...DEFAULT_VALUES,
      certificateFiles: initialCertificateFile ? [initialCertificateFile] : [],
    },
    mode: "onSubmit",
  });

  const sections = useMemo(() => getUpdateNodesSections(t), [t]);

  const handleSubmit = useCallback(
    (values: UpdateNodesFormValues) => {
      orchestration.startFlow(mapFormValuesToMutationParams(values));
    },
    [orchestration],
  );

  const goToRegistrationJobs = useCallback(() => {
    orchestration.closeDialog();
    void navigate({ to: "/home/node-management/update" });
  }, [orchestration, navigate]);

  const updateMore = useCallback(() => {
    orchestration.closeDialog();
    form.reset(DEFAULT_VALUES);
  }, [orchestration, form]);

  const handleDialogOpenChange = useCallback(
    (open: boolean) => {
      if (open) {return;}
      goToRegistrationJobs();
    },
    [goToRegistrationJobs],
  );

  return {
    t,
    form,
    sections,
    processState: orchestration.processState,
    showFooter: orchestration.showFooter,
    isSubmitting:
      orchestration.isSubmitting || orchestration.processState.dialogOpen,
    handleSubmit,
    handleDialogOpenChange,
    retryFrom: orchestration.retryFrom,
    closeDialog: orchestration.closeDialog,
    goToRegistrationJobs,
    updateMore,
  };
}
