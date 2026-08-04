/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useSetState } from "react-use";
import { useCreateOtaJobMutation } from "@/api/ota-jobs";
import { otaImagesQueries } from "@/api/ota-images";
import type { CreateOtaJobFormValues } from "../_schema/create-ota-job-form.schema";
import { buildCreateOtaJobPayload } from "../_utils/build-create-ota-job-payload";

export type CreateOtaJobStatus = "idle" | "creating" | "done" | "error";

/** Shape consumed by the success card. */
export interface CreateOtaJobResult {
  jobId: string;
  name: string;
}

interface OrchestrationState {
  status: CreateOtaJobStatus;
  dialogOpen: boolean;
  errorMessage: string;
  result: CreateOtaJobResult | null;
}

const initialState: OrchestrationState = {
  status: "idle",
  dialogOpen: false,
  errorMessage: "",
  result: null,
};

// The IoT job can be created quickly; hold the "creating" state for a minimum
// so the fake progress bar stays visible (mirrors the Upload OTA Image flow).
const MIN_CREATING_MS = 1000;

const delay = (ms: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, ms));

export function useCreateOtaJobOrchestration() {
  const { t } = useTranslation("ota-jobs");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const mutation = useCreateOtaJobMutation();
  const [state, setState] = useSetState<OrchestrationState>(initialState);

  const normalizeError = useCallback(
    (error: unknown): string => {
      const message = error instanceof Error ? error.message : String(error);
      if (message.includes("ResourceAlreadyExists")) {
        return t(
          "createOtaJobPage.status.duplicateError",
          "An OTA job with this name already exists.",
        );
      }
      return (
        message ||
        t("createOtaJobPage.status.errorDescription", "Please try again.")
      );
    },
    [t],
  );

  const startFlow = useCallback(
    async (values: CreateOtaJobFormValues) => {
      setState({
        status: "creating",
        dialogOpen: true,
        errorMessage: "",
        result: null,
      });

      // Best-effort version lookup from the image's S3 tags. A tags failure must
      // never block job creation, so fall back to an undefined version.
      const tags = await queryClient
        .fetchQuery(otaImagesQueries.firmwareTags(values.firmwareKey))
        .catch(() => ({ version: undefined }));

      const payload = buildCreateOtaJobPayload(values, tags.version);

      try {
        const [{ jobId }] = await Promise.all([
          mutation.mutateAsync(payload),
          delay(MIN_CREATING_MS),
        ]);

        setState({
          status: "done",
          result: { jobId, name: values.name },
        });
      } catch (error) {
        setState({
          status: "error",
          errorMessage: normalizeError(error),
        });
      }
    },
    [mutation, queryClient, setState, normalizeError],
  );

  // Closes the dialog and returns to the form with the entered values intact so
  // the user can fix the offending field — e.g. a duplicate name — and resubmit.
  const reset = useCallback(() => {
    mutation.reset();
    setState({ ...initialState });
  }, [mutation, setState]);

  const backToJobs = useCallback(() => {
    void navigate({ to: "/home/ota/jobs" });
  }, [navigate]);

  const viewJobDetails = useCallback(() => {
    if (!state.result) {
      return;
    }
    void navigate({
      to: "/home/ota/jobs/$jobId",
      params: { jobId: state.result.jobId },
    });
  }, [navigate, state.result]);

  return {
    status: state.status,
    dialogOpen: state.dialogOpen,
    errorMessage: state.errorMessage,
    result: state.result,
    startFlow,
    reset,
    backToJobs,
    viewJobDetails,
  };
}
