/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import {
  buildCreateNodeGroupFormSchema,
  getCreateNodeGroupFormSchemaMessages,
  type CreateNodeGroupFormValues,
} from "../_schema/create-node-group-form.schema";
import { getCreateNodeGroupSections } from "../_config/create-node-group-sections.config";
import { useCreateNodeGroupOrchestration } from "./use-create-node-group-orchestration";

const DEFAULT_VALUES: CreateNodeGroupFormValues = {
  groupName: "",
  description: "",
  createAsSubgroup: false,
  parentGroupName: "",
  createAsDynamic: false,
  queryRules: [],
};

export function useCreateNodeGroupForm() {
  const { t } = useTranslation("node-groups");

  const schema = useMemo(
    () => buildCreateNodeGroupFormSchema(getCreateNodeGroupFormSchemaMessages(t)),
    [t],
  );

  const form = useForm<CreateNodeGroupFormValues>({
    resolver: zodResolver(schema),
    defaultValues: DEFAULT_VALUES,
    mode: "onSubmit",
  });

  const sections = useMemo(() => getCreateNodeGroupSections(t), [t]);

  const {
    status,
    dialogOpen,
    errorMessage,
    result,
    startFlow,
    reset,
    backToGroups,
    viewGroupDetails,
  } = useCreateNodeGroupOrchestration();

  const handleSubmit = useCallback(
    (values: CreateNodeGroupFormValues) => {
      void startFlow(values);
    },
    [startFlow],
  );

  const createAnother = useCallback(() => {
    reset();
    form.reset(DEFAULT_VALUES);
  }, [reset, form]);

  // While creating or after a failure, keep the entered values so the user can
  // fix a field (e.g. a duplicate name) and resubmit; after success the values
  // are stale, so clear the form.
  const handleDialogOpenChange = useCallback(
    (open: boolean) => {
      if (open) {
        return;
      }
      if (status === "done") {
        createAnother();
      } else {
        reset();
      }
    },
    [status, createAnother, reset],
  );

  return {
    t,
    form,
    sections,
    status,
    dialogOpen,
    errorMessage,
    result,
    isSubmitting: status === "creating",
    handleSubmit,
    handleDialogOpenChange,
    backToGroups,
    viewGroupDetails,
    // Error-state action: close the dialog, keep the form values so the user can
    // edit the offending field and resubmit.
    editAndRetry: reset,
  };
}
