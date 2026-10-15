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
  UpdateNodesParams,
  UpdateJobStatusResponse,
  UpdateStepId,
  UpdateStepMessages,
} from "@/api/node-update";
import {
  deriveUpdateResultAlert,
  isTerminalStatus,
  readJobCounts,
  type UpdateResultAlertData,
  type UpdateResultKind,
} from "../../_utils/update-result.utils";
import type { UpdateNodesFormValues } from "../../_schema/update-nodes-form.schema";

export type PopupStepId = UpdateStepId | "request-completed";

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

export function buildUpdateTagsFromForm(tags: string[]): string[] {
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
  values: UpdateNodesFormValues,
): Omit<UpdateNodesParams, "onProgress" | "stepMessages"> {
  const file = values.certificateFiles[0];
  if (!file) {
    throw new Error("No certificate file selected");
  }

  const params: Omit<UpdateNodesParams, "onProgress" | "stepMessages"> = {
    file,
    tags: buildUpdateTagsFromForm(values.tags),
  };

  if (values.subgroupName) {
    params.adminGroupNames = [values.subgroupName];
    if (values.groupName) {
      params.adminParentGroupName = values.groupName;
    }
  } else if (values.groupName) {
    params.adminGroupNames = [values.groupName];
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
      title: t("new.progress.initiateTitle", "Initiate update"),
      description,
      state: "not_started",
    },
    {
      id: "upload-file",
      icon: createElement(CloudUploadIcon, { className: "h-4 w-4" }),
      title: t("new.progress.uploadTitle", "Upload node file"),
      description,
      state: "not_started",
    },
    {
      id: "create-request",
      icon: createElement(FileTextIcon, { className: "h-4 w-4" }),
      title: t(
        "new.progress.createTitle",
        "Create update request",
      ),
      description,
      state: "not_started",
    },
    {
      id: "request-completed",
      icon: createElement(CircleCheckIcon, { className: "h-4 w-4" }),
      title: t(
        "new.progress.completedTitle",
        "Update completed",
      ),
      description,
      state: "not_started",
    },
  ];
}

export function buildStepMessages(
  t: TFunction,
): Record<UpdateStepId, UpdateStepMessages> {
  return {
    "initiate-registration": {
      inProgress: t(
        "new.progress.initiateInProgress",
        "Requesting a secure upload URL…",
      ),
      success: t(
        "new.progress.initiateSuccess",
        "Update initiated",
      ),
      errorFallback: t(
        "new.progress.initiateError",
        "Could not initiate update",
      ),
    },
    "upload-file": {
      inProgress: t(
        "new.progress.uploadInProgress",
        "Uploading CSV…",
      ),
      success: t(
        "new.progress.uploadSuccess",
        "CSV uploaded",
      ),
      errorFallback: t(
        "new.progress.uploadError",
        "Could not upload CSV",
      ),
    },
    "create-request": {
      inProgress: t(
        "new.progress.createInProgress",
        "Submitting update request…",
      ),
      success: t(
        "new.progress.createSuccess",
        "Update request created",
      ),
      errorFallback: t(
        "new.progress.createError",
        "Could not create update request",
      ),
    },
  };
}

export interface Step4Outcome {
  state: StatusCardState;
  description: string;
  resultAlert: UpdateResultAlertData | null;
}

/** `StatusCardState` has no "warning", so a partial result still reads as an error row. */
const STEP4_STATE_BY_KIND: Record<UpdateResultKind, StatusCardState> = {
  success: "success",
  partial: "error",
  "all-failed": "error",
  "job-error": "error",
  "status-error": "error",
};

export function mapPollingResponseToStep4(
  response: UpdateJobStatusResponse | undefined,
  t: TFunction<"update">,
): Step4Outcome {
  if (!response || !isTerminalStatus(response.status)) {
    return {
      state: "in_progress",
      description: t(
        "new.progress.completedInProgress",
        "Updating nodes… this can take a moment",
      ),
      resultAlert: null,
    };
  }

  const resultAlert = deriveUpdateResultAlert(response, t);
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
  kind: UpdateResultKind,
  response: UpdateJobStatusResponse,
  t: TFunction<"update">,
): string {
  if (kind === "success") {
    return t("new.progress.completedSuccess", "All nodes updated");
  }
  if (kind === "all-failed") {
    return t("new.progress.completedAllFailed", "No nodes were updated");
  }
  if (kind === "partial") {
    const { failedCount, totalNodes, countsKnown } = readJobCounts(response);
    if (!countsKnown) {
      return t(
        "new.progress.completedWithUnknownFailures",
        "Some nodes failed to update",
      );
    }
    return t(
      "new.progress.completedWithFailures",
      "{{failedCount}} of {{totalNodes}} nodes failed to update",
      { failedCount, totalNodes },
    );
  }
  return t("new.progress.completedError", "Update failed");
}

export function errorDescription(
  error: unknown,
  fallback: string,
): string {
  if (error instanceof Error && error.message) {return error.message;}
  return fallback;
}
