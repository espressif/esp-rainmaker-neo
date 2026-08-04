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
  buildCreateOtaJobFormSchema,
  getCreateOtaJobFormSchemaMessages,
  type CreateOtaJobFormValues,
} from "../_schema/create-ota-job-form.schema";
import { getCreateOtaJobSections } from "../_config/create-ota-job-sections.config";
import {
  JOB_MODE_SNAPSHOT,
  TARGET_TYPE_GROUP,
} from "../_constants/create-ota-job-form.constants";
import { useCreateOtaJobOrchestration } from "./use-create-ota-job-orchestration";

const BASE_DEFAULT_VALUES: CreateOtaJobFormValues = {
  name: "",
  firmwareKey: "",
  fileMd5: "",
  targetType: TARGET_TYPE_GROUP,
  targetSelection: JOB_MODE_SNAPSHOT,
  source: "",
  targetName: "",
  queryRules: [],
};

// Seed only the firmware image from a deep link (`?firmware_key=`); the S3
// selector reconciles it against the loaded list on mount and drops it if the
// key no longer exists.
function getCreateOtaJobFormDefaultValues(
  firmwareKey?: string,
): CreateOtaJobFormValues {
  return { ...BASE_DEFAULT_VALUES, firmwareKey: firmwareKey ?? "" };
}

export function useCreateOtaJobForm(firmwareKey?: string) {
  const { t } = useTranslation("ota-jobs");

  const schema = useMemo(
    () => buildCreateOtaJobFormSchema(getCreateOtaJobFormSchemaMessages(t)),
    [t],
  );

  const defaultValues = useMemo(
    () => getCreateOtaJobFormDefaultValues(firmwareKey),
    [firmwareKey],
  );

  const form = useForm<CreateOtaJobFormValues>({
    resolver: zodResolver(schema),
    defaultValues,
    mode: "onSubmit",
  });

  const sections = useMemo(() => getCreateOtaJobSections(t), [t]);

  const {
    status,
    dialogOpen,
    errorMessage,
    result,
    startFlow,
    reset,
    backToJobs,
    viewJobDetails,
  } = useCreateOtaJobOrchestration();

  const handleSubmit = useCallback(
    (values: CreateOtaJobFormValues) => {
      void startFlow(values);
    },
    [startFlow],
  );

  const createAnother = useCallback(() => {
    reset();
    // A fresh "create another" starts blank — don't re-apply the deep-link key.
    form.reset(getCreateOtaJobFormDefaultValues());
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
    backToJobs,
    viewJobDetails,
    // Error-state action: close the dialog, keep the form values so the user can
    // edit the offending field and resubmit.
    editAndRetry: reset,
  };
}
