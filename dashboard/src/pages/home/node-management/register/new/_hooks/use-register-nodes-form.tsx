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
  buildRegisterNodesFormSchema,
  getRegisterNodesFormSchemaMessages,
  type RegisterNodesFormValues,
} from "../_schema/register-nodes-form.schema";
import { getRegisterNodesSections } from "../_config/sections.config";
import { mapFormValuesToMutationParams } from "../_components/register-nodes-form/register-nodes-form.utils";
import { useRegistrationOrchestration } from "./use-registration-orchestration";
import { getExtraCapabilities } from "@/lib/config";
import { useEnterpriseStore } from "@/enterprise/enterprise.store";
import { collectEnterpriseSections } from "@/enterprise/extension-sections";

/** Section-shaped extension slot: a plugin contribution here becomes a whole form section. */
const SECTIONS_SLOT_ID = "nodes.register.sections";

const DEFAULT_VALUES: RegisterNodesFormValues = {
  certificateFiles: [],
  groupName: undefined,
  subgroupName: undefined,
  capabilities: [],
  tags: [],
};

export function useRegisterNodesForm(initialCertificateFile?: File) {
  const { t } = useTranslation("nodes");
  const navigate = useNavigate();
  const orchestration = useRegistrationOrchestration();

  // Deploy-time contributed capability ids join the allowed set (config store
  // hydrates before render, matching how the cards themselves are gated).
  const extraCapabilityIds = useMemo(
    () => getExtraCapabilities().map((capability) => capability.id),
    [],
  );

  const schema = useMemo(
    () =>
      buildRegisterNodesFormSchema(
        getRegisterNodesFormSchemaMessages(t),
        extraCapabilityIds,
      ),
    [t, extraCapabilityIds],
  );

  const form = useForm<RegisterNodesFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      ...DEFAULT_VALUES,
      certificateFiles: initialCertificateFile ? [initialCertificateFile] : [],
    },
    mode: "onSubmit",
  });

  const enterpriseFeatures = useEnterpriseStore((s) => s.features);
  const sections = useMemo(
    () => [
      ...getRegisterNodesSections(t),
      ...collectEnterpriseSections(enterpriseFeatures, SECTIONS_SLOT_ID, t, { form }),
    ],
    [t, enterpriseFeatures, form],
  );

  const handleSubmit = useCallback(
    (values: RegisterNodesFormValues) => {
      orchestration.startFlow(mapFormValuesToMutationParams(values));
    },
    [orchestration],
  );

  const goToRegistrationJobs = useCallback(() => {
    orchestration.closeDialog();
    void navigate({ to: "/home/node-management/register" });
  }, [orchestration, navigate]);

  const registerMore = useCallback(() => {
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
    registerMore,
  };
}
