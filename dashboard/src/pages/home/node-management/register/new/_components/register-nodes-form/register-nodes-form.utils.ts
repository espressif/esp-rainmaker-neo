/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { createElement } from "react";
import type { TFunction } from "i18next";
import type {
  StatusCardListItem,
  StatusCardState,
} from "@espressif/dashboard-ui-components/components";
import {
  CircleCheckIcon,
  CloudUploadIcon,
  FileTextIcon,
  KeyRoundIcon,
} from "lucide-react";
import type {
  RegisterNodesParams,
  RegistrationJobStatusResponse,
  RegistrationStepId,
  RegistrationStepMessages,
} from "@/api/node-registration";
import {
  deriveRegistrationResultAlert,
  isTerminalStatus,
  readJobCounts,
  type RegistrationResultAlertData,
  type RegistrationResultKind,
} from "../../_utils/registration-result.utils";
import type { RegisterNodesFormValues } from "../../_schema/register-nodes-form.schema";

export type PopupStepId = RegistrationStepId | "request-completed";

const DEFAULT_TAG_KEYS = {
  registeredFrom: "registered_from",
  batch: "batch",
} as const;

function buildBatchTimestamp(): string {
  const now = new Date();
  return (
    now.getFullYear().toString() +
    String(now.getMonth() + 1).padStart(2, "0") +
    String(now.getDate()).padStart(2, "0") +
    "-" +
    String(now.getHours()).padStart(2, "0") +
    ":" +
    String(now.getMinutes()).padStart(2, "0") +
    ":" +
    String(now.getSeconds()).padStart(2, "0")
  );
}

export function buildRegistrationTagsFromForm(tags: string[]): string[] {
  const merged = new Map<string, string>();
  merged.set(DEFAULT_TAG_KEYS.registeredFrom, "dashboard");
  merged.set(DEFAULT_TAG_KEYS.batch, buildBatchTimestamp());
  for (const raw of tags) {
    const separator = raw.indexOf(":");
    if (separator <= 0 || separator === raw.length - 1) {continue;}
    const key = raw.slice(0, separator).trim();
    const value = raw.slice(separator + 1).trim();
    if (!key || !value) {continue;}
    merged.set(key, value);
  }
  return Array.from(merged.entries()).map(([k, v]) => `${k}:${v}`);
}

export function mapFormValuesToMutationParams(
  values: RegisterNodesFormValues,
): Omit<RegisterNodesParams, "onProgress" | "stepMessages"> {
  const file = values.certificateFiles[0];
  if (!file) {
    throw new Error("No certificate file selected");
  }

  const params: Omit<RegisterNodesParams, "onProgress" | "stepMessages"> = {
    file,
    tags: buildRegistrationTagsFromForm(values.tags),
  };

  if (values.subgroupName) {
    params.adminGroupNames = [values.subgroupName];
    if (values.groupName) {
      params.adminParentGroupName = values.groupName;
    }
  } else if (values.groupName) {
    params.adminGroupNames = [values.groupName];
  }

  if (values.capabilities.length > 0) {
    params.capabilities = [...values.capabilities];
  }

  return params;
}

function notStartedDescription(t: TFunction) {
  return createElement(
    "em",
    null,
    t("new.progress.notStarted", "Not started yet"),
  );
}

export function buildInitialSteps(t: TFunction): StatusCardListItem[] {
  const description = notStartedDescription(t);
  return [
    {
      id: "initiate-registration",
      icon: createElement(KeyRoundIcon, { className: "h-4 w-4" }),
      title: t("new.progress.initiateTitle", "Initiate registration"),
      description,
      state: "not_started",
    },
    {
      id: "upload-file",
      icon: createElement(CloudUploadIcon, { className: "h-4 w-4" }),
      title: t("new.progress.uploadTitle", "Upload certificate file"),
      description,
      state: "not_started",
    },
    {
      id: "create-request",
      icon: createElement(FileTextIcon, { className: "h-4 w-4" }),
      title: t(
        "new.progress.createTitle",
        "Create registration request",
      ),
      description,
      state: "not_started",
    },
    {
      id: "request-completed",
      icon: createElement(CircleCheckIcon, { className: "h-4 w-4" }),
      title: t(
        "new.progress.completedTitle",
        "Registration completed",
      ),
      description,
      state: "not_started",
    },
  ];
}

export function buildStepMessages(
  t: TFunction,
): Record<RegistrationStepId, RegistrationStepMessages> {
  return {
    "initiate-registration": {
      inProgress: t(
        "new.progress.initiateInProgress",
        "Requesting a secure upload URL…",
      ),
      success: t(
        "new.progress.initiateSuccess",
        "Registration initiated",
      ),
      errorFallback: t(
        "new.progress.initiateError",
        "Could not initiate registration",
      ),
    },
    "upload-file": {
      inProgress: t(
        "new.progress.uploadInProgress",
        "Uploading certificate CSV…",
      ),
      success: t(
        "new.progress.uploadSuccess",
        "Certificate file uploaded",
      ),
      errorFallback: t(
        "new.progress.uploadError",
        "Could not upload certificate file",
      ),
    },
    "create-request": {
      inProgress: t(
        "new.progress.createInProgress",
        "Submitting registration request…",
      ),
      success: t(
        "new.progress.createSuccess",
        "Registration request created",
      ),
      errorFallback: t(
        "new.progress.createError",
        "Could not create registration request",
      ),
    },
  };
}

export interface Step4Outcome {
  state: StatusCardState;
  description: string;
  resultAlert: RegistrationResultAlertData | null;
}

/** `StatusCardState` has no "warning", so a partial result still reads as an error row. */
const STEP4_STATE_BY_KIND: Record<RegistrationResultKind, StatusCardState> = {
  success: "success",
  partial: "error",
  "all-failed": "error",
  "job-error": "error",
  "status-error": "error",
};

export function mapPollingResponseToStep4(
  response: RegistrationJobStatusResponse | undefined,
  t: TFunction<"nodes">,
): Step4Outcome {
  if (!response || !isTerminalStatus(response.status)) {
    return {
      state: "in_progress",
      description: t(
        "new.progress.completedInProgress",
        "Registering nodes… this can take a moment",
      ),
      resultAlert: null,
    };
  }

  const resultAlert = deriveRegistrationResultAlert(response, t);
  return {
    state: STEP4_STATE_BY_KIND[resultAlert.kind],
    description: buildStep4Description(resultAlert.kind, response, t),
    resultAlert,
  };
}

/**
 * Short one-liner for the step row — `StatusCardList` truncates to a single line,
 * so the detail lives in the result alert instead.
 */
function buildStep4Description(
  kind: RegistrationResultKind,
  response: RegistrationJobStatusResponse,
  t: TFunction<"nodes">,
): string {
  if (kind === "success") {
    return t("new.progress.completedSuccess", "All nodes registered");
  }
  if (kind === "all-failed") {
    return t("new.progress.completedAllFailed", "No nodes were registered");
  }
  if (kind === "partial") {
    const { failedCount, totalNodes, countsKnown } = readJobCounts(response);
    if (!countsKnown) {
      return t(
        "new.progress.completedWithUnknownFailures",
        "Some nodes failed to register",
      );
    }
    return t(
      "new.progress.completedWithFailures",
      "{{failedCount}} of {{totalNodes}} nodes failed to register",
      { failedCount, totalNodes },
    );
  }
  return t("new.progress.completedError", "Registration failed");
}

export function errorDescription(
  error: unknown,
  fallback: string,
): string {
  if (error instanceof Error && error.message) {return error.message;}
  return fallback;
}
