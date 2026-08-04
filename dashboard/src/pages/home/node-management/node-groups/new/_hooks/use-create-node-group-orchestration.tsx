/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import { useSetState } from "react-use";
import { useCreateNodeGroupMutation } from "@/api/node-groups";
import type { CreateNodeGroupFormValues } from "../_schema/create-node-group-form.schema";
import { buildCreateNodeGroupRequest } from "../_utils/build-create-node-group-request";

export type CreateNodeGroupStatus = "idle" | "creating" | "done" | "error";

/** Shape consumed by the success card for navigation. */
export interface CreateNodeGroupResult {
  groupName: string;
}

interface OrchestrationState {
  status: CreateNodeGroupStatus;
  dialogOpen: boolean;
  errorMessage: string;
  result: CreateNodeGroupResult | null;
}

const initialState: OrchestrationState = {
  status: "idle",
  dialogOpen: false,
  errorMessage: "",
  result: null,
};

// The group is created quickly; hold the "creating" state for a minimum so the
// fake progress bar stays visible (mirrors the Create OTA Job flow).
const MIN_CREATING_MS = 1000;

const delay = (ms: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, ms));

export function useCreateNodeGroupOrchestration() {
  const { t } = useTranslation("node-groups");
  const navigate = useNavigate();
  const mutation = useCreateNodeGroupMutation();
  const [state, setState] = useSetState<OrchestrationState>(initialState);

  const normalizeError = useCallback(
    (error: unknown): string => {
      const message = error instanceof Error ? error.message : String(error);
      if (message.includes("ResourceAlreadyExists")) {
        return t(
          "new.status.duplicateError",
          "A node group with this name already exists.",
        );
      }
      return (
        message || t("new.status.errorDescription", "Please try again.")
      );
    },
    [t],
  );

  const startFlow = useCallback(
    async (values: CreateNodeGroupFormValues) => {
      setState({
        status: "creating",
        dialogOpen: true,
        errorMessage: "",
        result: null,
      });

      try {
        const [{ groupName }] = await Promise.all([
          mutation.mutateAsync(buildCreateNodeGroupRequest(values)),
          delay(MIN_CREATING_MS),
        ]);

        setState({ status: "done", result: { groupName } });
      } catch (error) {
        setState({ status: "error", errorMessage: normalizeError(error) });
      }
    },
    [mutation, setState, normalizeError],
  );

  // Closes the dialog and returns to the form with the entered values intact so
  // the user can fix the offending field — e.g. a duplicate name — and resubmit.
  const reset = useCallback(() => {
    mutation.reset();
    setState({ ...initialState });
  }, [mutation, setState]);

  const backToGroups = useCallback(() => {
    void navigate({ to: "/home/node-management/node-groups" });
  }, [navigate]);

  const viewGroupDetails = useCallback(() => {
    if (!state.result) {
      return;
    }
    void navigate({
      to: "/home/node-management/node-groups/$groupName",
      params: { groupName: state.result.groupName },
    });
  }, [navigate, state.result]);

  return {
    status: state.status,
    dialogOpen: state.dialogOpen,
    errorMessage: state.errorMessage,
    result: state.result,
    startFlow,
    reset,
    backToGroups,
    viewGroupDetails,
  };
}
