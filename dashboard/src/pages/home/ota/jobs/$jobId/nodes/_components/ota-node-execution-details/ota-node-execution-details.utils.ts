/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type { JobExecution } from "@aws-sdk/client-iot";
import type {
  DynamicListEntry,
  DynamicListMetaEntry,
} from "@espressif/dashboard-ui-components/components";

/** Device-reported detail keys that read better as a full-width info block. */
const INFO_DETAIL_KEYS = new Set(["reason"]);

/** Fixed execution fields surfaced above the dynamic `statusDetails` map. */
export function buildExecutionItems(
  execution: JobExecution,
): DynamicListEntry[] {
  const detailsMap = execution.statusDetails?.detailsMap ?? {};
  const entries: DynamicListEntry[] = [
    { key: "executionNumber", value: execution.executionNumber },
    { key: "versionNumber", value: execution.versionNumber },
    { key: "queuedAt", value: execution.queuedAt?.getTime() },
    { key: "startedAt", value: execution.startedAt?.getTime() },
    { key: "lastUpdatedAt", value: execution.lastUpdatedAt?.getTime() },
    ...Object.entries(detailsMap).map(([key, value]) => ({ key, value })),
  ];
  return entries.filter(
    ({ value }) => value !== undefined && value !== null && value !== "",
  );
}

export function buildExecutionMeta(
  execution: JobExecution,
  t: TFunction,
): Record<string, DynamicListMetaEntry> {
  const detailsMap = execution.statusDetails?.detailsMap ?? {};
  const meta: Record<string, DynamicListMetaEntry> = {
    executionNumber: {
      label: t(
        "details.nodes.executionDetail.fields.executionNumber",
        "Execution number",
      ),
    },
    versionNumber: {
      label: t(
        "details.nodes.executionDetail.fields.versionNumber",
        "Version number",
      ),
    },
    queuedAt: {
      label: t("details.nodes.executionDetail.fields.queuedAt", "Queued at"),
      type: "timestamp",
    },
    startedAt: {
      label: t("details.nodes.executionDetail.fields.startedAt", "Started at"),
      type: "timestamp",
    },
    lastUpdatedAt: {
      label: t(
        "details.nodes.executionDetail.fields.lastUpdatedAt",
        "Last updated at",
      ),
      type: "timestamp",
    },
  };
  // `statusDetails.detailsMap` keys are device-reported (e.g. `fw_version`,
  // `reason`); render `reason` as a full-width info block and the rest as
  // monospace values with a start-cased label.
  for (const key of Object.keys(detailsMap)) {
    meta[key] = { type: INFO_DETAIL_KEYS.has(key) ? "info" : "mono" };
  }
  return meta;
}
