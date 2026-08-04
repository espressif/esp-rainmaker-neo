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
  buildUploadOtaImageFormSchema,
  getUploadOtaImageFormSchemaMessages,
  type UploadOtaImageFormValues,
} from "../_schema/upload-ota-image-form.schema";
import { getUploadOtaImageSections } from "../_config/upload-ota-image-sections.config";
import { useUploadOtaImageOrchestration } from "./use-upload-ota-image-orchestration";

const DEFAULT_VALUES: UploadOtaImageFormValues = {
  firmwareFiles: [],
  name: "",
  version: "",
  type: "",
  model: "",
  platform: "",
};

export function useUploadOtaImageForm() {
  const { t } = useTranslation("ota-jobs");

  const schema = useMemo(
    () => buildUploadOtaImageFormSchema(getUploadOtaImageFormSchemaMessages(t)),
    [t],
  );

  const form = useForm<UploadOtaImageFormValues>({
    resolver: zodResolver(schema),
    defaultValues: DEFAULT_VALUES,
    mode: "onSubmit",
  });

  const sections = useMemo(() => getUploadOtaImageSections(t), [t]);

  const {
    status,
    dialogOpen,
    errorMessage,
    result,
    startFlow,
    reset,
    backToImages,
    createOtaWithImage,
  } = useUploadOtaImageOrchestration();

  const handleSubmit = useCallback(
    (values: UploadOtaImageFormValues) => {
      void startFlow(values);
    },
    [startFlow],
  );

  const uploadAnother = useCallback(() => {
    reset();
    form.reset(DEFAULT_VALUES);
  }, [reset, form]);

  // Closing the dialog mirrors the state's primary intent: after a successful
  // upload the values are stale (resubmitting them would only hit a duplicate),
  // so clear the form; while uploading or after a failure, keep them so the user
  // can fix a field and resubmit.
  const handleDialogOpenChange = useCallback(
    (open: boolean) => {
      if (open) {
        return;
      }
      if (status === "done") {
        uploadAnother();
      } else {
        reset();
      }
    },
    [status, uploadAnother, reset],
  );

  return {
    t,
    form,
    sections,
    status,
    dialogOpen,
    errorMessage,
    result,
    isUploading: status === "uploading",
    handleSubmit,
    handleDialogOpenChange,
    backToImages,
    createOtaWithImage,
    // Error-state action: close the dialog, keep the form values so the user
    // can edit the offending field and resubmit.
    editAndRetry: reset,
  };
}
