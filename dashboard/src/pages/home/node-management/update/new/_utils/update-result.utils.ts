/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type { AlertType } from "@espressif/dashboard-ui-components/components";
import type { UpdateJobStatusResponse } from "@/api/node-update";

const TERMINAL_STATUSES = new Set(["success", "completed", "error", "failed"]);

const ERROR_STATUSES = new Set(["error", "failed"]);

export function isTerminalStatus(status: string | undefined): boolean {
  return !!status && TERMINAL_STATUSES.has(status);
}

export function isErroredJobStatus(status: string | undefined): boolean {
  return !!status && ERROR_STATUSES.has(status);
}

export type UpdateResultKind =
  | "success"
  | "partial"
  | "all-failed"
  | "job-error"
  | "status-error";

export interface UpdateResultAlertData {
  kind: UpdateResultKind;
  type: AlertType;
  title: string;
  description: string;
  /**
   * S3 path of the re-uploadable failed-rows CSV. The backend writes it only when
   * the job finished with failures *and* its own S3 write succeeded, so a failure
   * result legitimately arrives without one.
   */
  failedFileS3Path?: string;
}

const I18N_PREFIX = "new.progress.resultAlert";

interface JobCounts {
  totalNodes: number;
  successCount: number;
  failedCount: number;
  /** `success_count` / `failed_count` are omitted until the job writes them. */
  countsKnown: boolean;
}

export function readJobCounts(
  response: UpdateJobStatusResponse,
): JobCounts {
  const successCount = response.success_count ?? 0;
  const failedCount = response.failed_count ?? 0;
  return {
    successCount,
    failedCount,
    // `total_nodes` can lag behind the per-node counters, so fall back to their sum.
    totalNodes: response.total_nodes || successCount + failedCount,
    countsKnown: typeof response.failed_count === "number",
  };
}

/** Precondition: `response.status` is terminal (see {@link isTerminalStatus}). */
export function deriveUpdateResultAlert(
  response: UpdateJobStatusResponse,
  t: TFunction<"update">,
): UpdateResultAlertData {
  const { totalNodes, successCount, failedCount, countsKnown } =
    readJobCounts(response);
  const failedFileS3Path = response.failed_file_s3_path;

  if (isErroredJobStatus(response.status)) {
    return {
      kind: "job-error",
      type: "error",
      title: t(`${I18N_PREFIX}.jobErrorTitle`, "Update job failed"),
      description:
        response.message?.trim() ||
        t(
          `${I18N_PREFIX}.jobErrorDescription`,
          "The update job stopped before it finished. Open it in Update jobs for details.",
        ),
      failedFileS3Path,
    };
  }

  // A failed-rows file exists only when something failed. Trusting the `?? 0`
  // default here would report a total failure as a success.
  if (!countsKnown && failedFileS3Path) {
    return {
      kind: "partial",
      type: "warning",
      title: t(`${I18N_PREFIX}.partialTitle`, "Some nodes could not be updated"),
      description: t(
        `${I18N_PREFIX}.partialDescriptionUnknownCounts`,
        "Some nodes could not be updated. Download the failed nodes file to see which ones, and why.",
      ),
      failedFileS3Path,
    };
  }

  if (failedCount === 0) {
    return {
      kind: "success",
      type: "success",
      title: t(`${I18N_PREFIX}.successTitle`, "All nodes updated successfully"),
      description: buildSuccessDescription(totalNodes, t),
    };
  }

  if (totalNodes > 0 && failedCount >= totalNodes) {
    return {
      kind: "all-failed",
      type: "error",
      title: t(`${I18N_PREFIX}.allFailedTitle`, "Failed to update nodes"),
      description: buildAllFailedDescription(totalNodes, !!failedFileS3Path, t),
      failedFileS3Path,
    };
  }

  return {
    kind: "partial",
    type: "warning",
    title: t(`${I18N_PREFIX}.partialTitle`, "Some nodes could not be updated"),
    description: buildPartialDescription(
      { totalNodes, successCount, failedCount },
      !!failedFileS3Path,
      t,
    ),
    failedFileS3Path,
  };
}

export function buildStatusErrorResultAlert(
  description: string,
  t: TFunction<"update">,
): UpdateResultAlertData {
  return {
    kind: "status-error",
    type: "error",
    title: t(
      `${I18N_PREFIX}.statusErrorTitle`,
      "Could not check update status",
    ),
    description,
  };
}

function buildSuccessDescription(
  totalNodes: number,
  t: TFunction<"update">,
): string {
  if (totalNodes > 0) {
    return t(
      `${I18N_PREFIX}.successDescription`,
      "All {{totalNodes}} nodes were updated.",
      { totalNodes },
    );
  }
  return t(
    `${I18N_PREFIX}.successDescriptionUnknownTotal`,
    "Every node in the CSV was updated.",
  );
}

function buildAllFailedDescription(
  totalNodes: number,
  hasFailedFile: boolean,
  t: TFunction<"update">,
): string {
  if (hasFailedFile) {
    return t(
      `${I18N_PREFIX}.allFailedDescriptionWithFile`,
      "None of the {{totalNodes}} nodes could be updated. Download the failed nodes file to see why.",
      { totalNodes },
    );
  }
  return t(
    `${I18N_PREFIX}.allFailedDescription`,
    "None of the {{totalNodes}} nodes could be updated. Open the job in Update jobs for details.",
    { totalNodes },
  );
}

function buildPartialDescription(
  counts: Omit<JobCounts, "countsKnown">,
  hasFailedFile: boolean,
  t: TFunction<"update">,
): string {
  if (hasFailedFile) {
    return t(
      `${I18N_PREFIX}.partialDescriptionWithFile`,
      "{{successCount}} of {{totalNodes}} nodes were updated. {{failedCount}} failed — download the failed nodes file to see why.",
      counts,
    );
  }
  return t(
    `${I18N_PREFIX}.partialDescription`,
    "{{successCount}} of {{totalNodes}} nodes were updated. {{failedCount}} failed.",
    counts,
  );
}
