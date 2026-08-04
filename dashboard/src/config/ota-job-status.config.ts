/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type {
  JobExecutionStatus,
  JobProcessDetails,
  JobStatus,
} from "@aws-sdk/client-iot";
import type { LucideIcon } from "lucide-react";
import {
  Activity,
  Ban,
  CheckCircle2,
  Clock,
  Hourglass,
  MinusCircle,
  TimerOff,
  Trash2,
  XCircle,
} from "lucide-react";
import type { Color } from "@espressif/dashboard-ui-components";
import {
  CHART_CATEGORY_COLORS,
  CHART_SERIES_COLORS,
} from "@espressif/dashboard-ui-components/components";

export interface OtaJobStatusPresentation {
  Icon: LucideIcon;
  color: Color;
  /**
   * Chart segment / accent color for this status. Chosen so terminal states
   * keep their intuitive hue (success→green, failed→red, timed out→amber, …)
   * while every status stays visually distinct. Semantic states resolve from
   * `CHART_CATEGORY_COLORS`; the rest use ordinal `CHART_SERIES_COLORS`.
   */
  chartColor?: string;
  /** i18n key under the `ota-jobs` namespace. `undefined` signals an unmapped status — the consumer falls back to the raw AWS string. */
  i18nKey?: string;
}

type OtaJobStatusKey = JobStatus | JobExecutionStatus;

export const OTA_JOB_STATUS_PRESENTATION: Record<OtaJobStatusKey, OtaJobStatusPresentation> = {
  IN_PROGRESS: {
    Icon: Activity,
    color: "primary",
    chartColor: CHART_CATEGORY_COLORS.primary,
    i18nKey: "common:otaJobStatus.IN_PROGRESS",
  },
  SCHEDULED: { Icon: Clock, color: "info", i18nKey: "common:otaJobStatus.SCHEDULED" },
  COMPLETED: { Icon: CheckCircle2, color: "success", i18nKey: "common:otaJobStatus.COMPLETED" },
  CANCELED: {
    Icon: Ban,
    color: "gray",
    chartColor: CHART_CATEGORY_COLORS.gray,
    i18nKey: "common:otaJobStatus.CANCELED",
  },
  DELETION_IN_PROGRESS: { Icon: Trash2, color: "warning", i18nKey: "common:otaJobStatus.DELETION_IN_PROGRESS" },
  SUCCEEDED: {
    Icon: CheckCircle2,
    color: "success",
    chartColor: CHART_CATEGORY_COLORS.success,
    i18nKey: "common:otaJobStatus.SUCCEEDED",
  },
  FAILED: {
    Icon: XCircle,
    color: "error",
    chartColor: CHART_CATEGORY_COLORS.error,
    i18nKey: "common:otaJobStatus.FAILED",
  },
  QUEUED: {
    Icon: Hourglass,
    color: "info",
    chartColor: CHART_SERIES_COLORS.color1,
    i18nKey: "common:otaJobStatus.QUEUED",
  },
  REJECTED: {
    Icon: Ban,
    color: "error",
    chartColor: CHART_SERIES_COLORS.color5,
    i18nKey: "common:otaJobStatus.REJECTED",
  },
  REMOVED: {
    Icon: MinusCircle,
    color: "gray",
    chartColor: CHART_SERIES_COLORS.color8,
    i18nKey: "common:otaJobStatus.REMOVED",
  },
  TIMED_OUT: {
    Icon: TimerOff,
    color: "warning",
    chartColor: CHART_CATEGORY_COLORS.warning,
    i18nKey: "common:otaJobStatus.TIMED_OUT",
  },
};

/**
 * Ordered iteration for the OTA jobs status filter dropdown. Only the five
 * job-level statuses accepted by the AWS IoT `ListJobs` `status` param —
 * execution statuses (SUCCEEDED/FAILED/…) are not valid filter values.
 */
export const OTA_JOB_STATUS_FILTER_IDS: readonly JobStatus[] = [
  "IN_PROGRESS",
  "SCHEDULED",
  "COMPLETED",
  "CANCELED",
  "DELETION_IN_PROGRESS",
];

const UNKNOWN_STATUS_PRESENTATION: OtaJobStatusPresentation = {
  Icon: Activity,
  color: "gray",
  chartColor: CHART_CATEGORY_COLORS.gray,
};

export function getOtaJobStatusPresentation(
  status?: string,
): OtaJobStatusPresentation {
  if (status && status in OTA_JOB_STATUS_PRESENTATION) {
    return OTA_JOB_STATUS_PRESENTATION[status as OtaJobStatusKey];
  }
  return UNKNOWN_STATUS_PRESENTATION;
}

export function getOtaJobStatusChartColor(status?: string): string {
  return (
    getOtaJobStatusPresentation(status).chartColor ??
    CHART_CATEGORY_COLORS.gray
  );
}

const CANCELABLE_STATUSES: ReadonlySet<JobStatus> = new Set([
  "IN_PROGRESS",
  "SCHEDULED",
]);

export function isCancelableJobStatus(status?: string): boolean {
  return !!status && CANCELABLE_STATUSES.has(status as JobStatus);
}

/** A job already being deleted (`DELETION_IN_PROGRESS`) can't be deleted again. */
export function isDeletableJobStatus(status?: string): boolean {
  return status !== "DELETION_IN_PROGRESS";
}

/** The numeric `numberOf*Things` counters (excludes `processingTargets`). */
type CountField = {
  [K in keyof JobProcessDetails]-?: NonNullable<
    JobProcessDetails[K]
  > extends number
    ? K
    : never;
}[keyof JobProcessDetails];

/**
 * Maps each AWS `jobProcessDetails` count to its execution status key so the
 * shared presentation (icon, color, chart color, label) can drive both the
 * Overview completion card and the Nodes tab status-summary chart. `SUCCEEDED`
 * leads because it is the headline metric surfaced above the fold.
 */
export const STATUS_COUNT_FIELDS: ReadonlyArray<{
  field: CountField;
  statusKey: JobExecutionStatus;
}> = [
  { field: "numberOfSucceededThings", statusKey: "SUCCEEDED" },
  { field: "numberOfInProgressThings", statusKey: "IN_PROGRESS" },
  { field: "numberOfQueuedThings", statusKey: "QUEUED" },
  { field: "numberOfFailedThings", statusKey: "FAILED" },
  { field: "numberOfRejectedThings", statusKey: "REJECTED" },
  { field: "numberOfTimedOutThings", statusKey: "TIMED_OUT" },
  { field: "numberOfCanceledThings", statusKey: "CANCELED" },
  { field: "numberOfRemovedThings", statusKey: "REMOVED" },
];

/** Ordered execution-status keys surfaced in the Nodes tab summary chart. */
export const OTA_JOB_EXECUTION_STATUS_KEYS: readonly JobExecutionStatus[] =
  STATUS_COUNT_FIELDS.map(({ statusKey }) => statusKey);

export interface OtaJobStatusCount {
  statusKey: JobExecutionStatus;
  count: number;
}

export function getOtaJobStatusCounts(
  details: JobProcessDetails | undefined,
): OtaJobStatusCount[] {
  return STATUS_COUNT_FIELDS.map(({ field, statusKey }) => ({
    statusKey,
    count: details?.[field] ?? 0,
  }));
}
