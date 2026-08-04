/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useEffect, useMemo, useRef } from "react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useSetState } from "react-use";
import type {
  StatusCardListItem,
  StatusCardState,
} from "@espressif/dashboard-ui-components/components";
import {
  useRegisterNodes,
  useRegistrationJobStatus,
  type RegisterNodesParams,
  type RegistrationStepId,
  type RegistrationStepState,
} from "@/api/node-registration";
import {
  buildInitialSteps,
  buildStepMessages,
  errorDescription,
  mapPollingResponseToStep4,
  type PopupStepId,
} from "../_components/register-nodes-form/register-nodes-form.utils";
import {
  buildStatusErrorResultAlert,
  type RegistrationResultAlertData,
} from "../_utils/registration-result.utils";

const STEP_ORDER: PopupStepId[] = [
  "initiate-registration",
  "upload-file",
  "create-request",
  "request-completed",
];

type MutationParams = Omit<RegisterNodesParams, "onProgress" | "stepMessages">;

interface StepPatch {
  state?: StatusCardState;
  description?: ReactNode;
}

export interface ProcessState {
  dialogOpen: boolean;
  steps: StatusCardListItem[];
  requestId: string | null;
  allDone: boolean;
  hasError: boolean;
  resultAlert: RegistrationResultAlertData | null;
}

const initialProcessState: ProcessState = {
  dialogOpen: false,
  steps: [],
  requestId: null,
  allDone: false,
  hasError: false,
  resultAlert: null,
};

function patchSteps(
  steps: StatusCardListItem[],
  patches: Record<string, StepPatch>,
): StatusCardListItem[] {
  return steps.map((step) => {
    const patch = patches[step.id];
    if (!patch) {return step;}
    const next: StatusCardListItem = { ...step };
    if (patch.state) {next.state = patch.state;}
    if (patch.description !== undefined) {next.description = patch.description;}
    return next;
  });
}

function useJobStatusSync(args: {
  requestId: string | null;
  jobStatusData: ReturnType<typeof useRegistrationJobStatus>["data"];
  jobStatusIsError: boolean;
  jobStatusError: unknown;
  setProcessState: (
    patch:
      | Partial<ProcessState>
      | ((prev: ProcessState) => Partial<ProcessState>),
  ) => void;
  t: ReturnType<typeof useTranslation<"register">>["t"];
}) {
  const {
    requestId,
    jobStatusData,
    jobStatusIsError,
    jobStatusError,
    setProcessState,
    t,
  } = args;

  useEffect(() => {
    if (!requestId) {return;}
    if (jobStatusIsError) {
      const detail = errorDescription(
        jobStatusError,
        t(
          "new.progress.resultAlert.statusErrorDescription",
          "We could not read the registration job status. The job may still be running — check Registration jobs.",
        ),
      );
      setProcessState((prev) => ({
        steps: patchSteps(prev.steps, {
          "request-completed": {
            state: "error",
            description: t(
              "new.progress.completedStatusUnavailable",
              "Could not check registration status",
            ),
          },
        }),
        allDone: true,
        hasError: true,
        resultAlert: buildStatusErrorResultAlert(detail, t),
      }));
      return;
    }
    const outcome = mapPollingResponseToStep4(jobStatusData, t);
    setProcessState((prev) => ({
      steps: patchSteps(prev.steps, {
        "request-completed": {
          state: outcome.state,
          description: outcome.description,
        },
      }),
      allDone: outcome.resultAlert !== null,
      hasError: false,
      resultAlert: outcome.resultAlert,
    }));
  }, [
    requestId,
    jobStatusData,
    jobStatusIsError,
    jobStatusError,
    setProcessState,
    t,
  ]);
}

export function useRegistrationOrchestration() {
  const { t } = useTranslation("register");
  const registerMutation = useRegisterNodes();
  const [processState, setProcessState] =
    useSetState<ProcessState>(initialProcessState);
  const lastSubmitParamsRef = useRef<MutationParams | null>(null);
  const stepMessages = useMemo(() => buildStepMessages(t), [t]);
  const jobStatusQuery = useRegistrationJobStatus(processState.requestId);
  const refetchJobStatus = jobStatusQuery.refetch;

  const closeDialog = useCallback(() => {
    registerMutation.reset();
    lastSubmitParamsRef.current = null;
    setProcessState({ ...initialProcessState });
  }, [registerMutation, setProcessState]);

  const updateMutationStep = useCallback(
    (
      stepId: RegistrationStepId,
      state: RegistrationStepState,
      description?: string,
    ) => {
      setProcessState((prev) => ({
        steps: patchSteps(prev.steps, { [stepId]: { state, description } }),
      }));
    },
    [setProcessState],
  );

  const runMutation = useCallback(
    (params: MutationParams) => {
      lastSubmitParamsRef.current = params;
      registerMutation.mutate(
        { ...params, onProgress: updateMutationStep, stepMessages },
        {
          onSuccess: ({ requestId }) => {
            setProcessState((prev) => ({
              requestId,
              steps: patchSteps(prev.steps, {
                "request-completed": {
                  state: "in_progress",
                  description: t(
                    "new.progress.completedInProgress",
                    "Registering nodes… this can take a moment",
                  ),
                },
              }),
            }));
          },
          onError: () => {
            setProcessState({ allDone: true, hasError: true });
          },
        },
      );
    },
    [registerMutation, updateMutationStep, stepMessages, setProcessState, t],
  );

  const retryFrom = useCallback(
    (stepId: PopupStepId) => {
      if (stepId === "request-completed") {
        setProcessState((prev) => ({
          allDone: false,
          hasError: false,
          resultAlert: null,
          steps: patchSteps(prev.steps, {
            "request-completed": {
              state: "in_progress",
              description: t(
                "new.progress.completedInProgress",
                "Registering nodes… this can take a moment",
              ),
            },
          }),
        }));
        void refetchJobStatus();
        return;
      }
      const params = lastSubmitParamsRef.current;
      if (!params) {return;}
      const fromIndex = STEP_ORDER.indexOf(stepId);
      const notStartedDesc = t(
        "new.progress.notStarted",
        "Not started yet",
      );
      const patches: Record<string, StepPatch> = {};
      for (let i = fromIndex; i < STEP_ORDER.length; i += 1) {
        patches[STEP_ORDER[i]] = {
          state: "not_started",
          description: notStartedDesc,
        };
      }
      setProcessState((prev) => ({
        steps: patchSteps(prev.steps, patches),
        allDone: false,
        hasError: false,
        resultAlert: null,
        requestId: null,
      }));
      registerMutation.reset();
      runMutation(params);
    },
    [refetchJobStatus, registerMutation, runMutation, setProcessState, t],
  );

  useJobStatusSync({
    requestId: processState.requestId,
    jobStatusData: jobStatusQuery.data,
    jobStatusIsError: jobStatusQuery.isError,
    jobStatusError: jobStatusQuery.error,
    setProcessState,
    t,
  });

  const startFlow = useCallback(
    (params: MutationParams) => {
      setProcessState({
        ...initialProcessState,
        dialogOpen: true,
        steps: buildInitialSteps(t),
      });
      runMutation(params);
    },
    [runMutation, setProcessState, t],
  );

  const handleDialogOpenChange = useCallback(
    (open: boolean) => {
      if (open) {return;}
      closeDialog();
    },
    [closeDialog],
  );

  return {
    processState,
    isSubmitting: registerMutation.isPending,
    startFlow,
    closeDialog,
    handleDialogOpenChange,
    retryFrom,
    showFooter: processState.allDone && !processState.hasError,
  };
}
