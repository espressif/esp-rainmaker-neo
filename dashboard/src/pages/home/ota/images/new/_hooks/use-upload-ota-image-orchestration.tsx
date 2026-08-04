/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import { useSetState } from "react-use";
import { useUploadOtaImageMutation } from "@/api/ota-images";
import type { UploadOtaImageFormValues } from "../_schema/upload-ota-image-form.schema";

export type UploadOtaImageStatus = "idle" | "uploading" | "done" | "error";

/** Shape consumed by the success card (Name / MD5 cell). */
export interface UploadOtaImageResult {
  /** Full S3 object key (e.g. `ota/light-sensor.bin`); deep-links the Create OTA Job flow. */
  key: string;
  name: string;
  fileSize: number;
  md5: string;
  fwType?: string;
}

interface OrchestrationState {
  status: UploadOtaImageStatus;
  dialogOpen: boolean;
  errorMessage: string;
  result: UploadOtaImageResult | null;
}

const initialState: OrchestrationState = {
  status: "idle",
  dialogOpen: false,
  errorMessage: "",
  result: null,
};

// The S3 PUT can resolve almost instantly for tiny files on fast connections, so
// hold the "uploading" state for a minimum to keep the fake progress bar visible
// (mirrors the Generate test nodes flow).
const MIN_UPLOADING_MS = 1000;

const delay = (ms: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, ms));

function isDuplicateError(message: string): boolean {
  return (
    message.includes("412") ||
    message.includes("PreconditionFailed") ||
    message.includes("pre-conditions")
  );
}

export function useUploadOtaImageOrchestration() {
  const { t } = useTranslation("ota-images");
  const navigate = useNavigate();
  const mutation = useUploadOtaImageMutation();
  const [state, setState] = useSetState<OrchestrationState>(initialState);

  const startFlow = useCallback(
    async (values: UploadOtaImageFormValues) => {
      setState({
        status: "uploading",
        dialogOpen: true,
        errorMessage: "",
        result: null,
      });

      const file = values.firmwareFiles[0];
      if (!file) {
        setState({
          status: "error",
          errorMessage: t(
            "errors.firmwareFileRequired",
            "Please select a firmware file.",
          ),
        });
        return;
      }

      try {
        const [uploadResult] = await Promise.all([
          mutation.mutateAsync({
            file,
            name: values.name,
            version: values.version,
            type: values.type,
            model: values.model,
            platform: values.platform,
          }),
          delay(MIN_UPLOADING_MS),
        ]);

        setState({
          status: "done",
          result: {
            key: uploadResult.key,
            name: uploadResult.key.replace(/^ota\//, ""),
            fileSize: uploadResult.fileSize,
            md5: uploadResult.md5,
            fwType: values.type || undefined,
          },
        });
      } catch (error) {
        const message =
          error instanceof Error ? error.message : String(error);
        setState({
          status: "error",
          errorMessage: isDuplicateError(message)
            ? t(
                "upload.duplicateError",
                "An OTA image with this name already exists.",
              )
            : message ||
              t("upload.errorDescription", "Please try again."),
        });
      }
    },
    [mutation, setState, t],
  );

  // Closes the dialog and returns to the form with the entered values intact
  // (react-hook-form state is untouched), so the user can fix the offending
  // field — e.g. a duplicate name — and resubmit, rather than blindly re-running
  // the same upload.
  const reset = useCallback(() => {
    mutation.reset();
    setState({ ...initialState });
  }, [mutation, setState]);

  const backToImages = useCallback(() => {
    void navigate({ to: "/home/ota/images" });
  }, [navigate]);

  // Deep-links into the Create OTA Job flow with the just-uploaded image
  // pre-selected. Mirrors the images-table "Start OTA" row action; falls back to
  // a blank form if the key is somehow absent rather than throwing.
  const createOtaWithImage = useCallback(() => {
    const firmwareKey = state.result?.key?.trim();
    if (!firmwareKey) {
      void navigate({ to: "/home/ota/jobs/new" });
      return;
    }
    void navigate({
      to: "/home/ota/jobs/new",
      search: { firmware_key: firmwareKey },
    });
  }, [navigate, state.result]);

  return {
    status: state.status,
    dialogOpen: state.dialogOpen,
    errorMessage: state.errorMessage,
    result: state.result,
    startFlow,
    reset,
    backToImages,
    createOtaWithImage,
  };
}
