/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type { JobProcessDetails } from "@aws-sdk/client-iot";
import type { ChartConfig } from "@espressif/dashboard-ui-components/components";
import {
  getOtaJobStatusChartColor,
  getOtaJobStatusCounts,
  getOtaJobStatusPresentation,
} from "@/config/ota-job-status.config";

/** Sum of every per-status count — AWS exposes no `total`. */
export function getOtaJobStatusTotal(
  details: JobProcessDetails | undefined,
): number {
  return getOtaJobStatusCounts(details).reduce(
    (sum, { count }) => sum + count,
    0,
  );
}

/**
 * The chart renders a single stacked horizontal bar, so the dataset is one row
 * keyed by status. Zero-count statuses are dropped to keep the bar and legend
 * free of empty segments.
 */
export function buildOtaJobStatusChartRow(
  details: JobProcessDetails | undefined,
): Record<string, number> {
  const row: Record<string, number> = {};
  for (const { statusKey, count } of getOtaJobStatusCounts(details)) {
    if (count > 0) {
      row[statusKey] = count;
    }
  }
  return row;
}

export function buildOtaJobStatusChartConfig(
  details: JobProcessDetails | undefined,
  t: TFunction,
): ChartConfig {
  const config: ChartConfig = {};
  for (const { statusKey, count } of getOtaJobStatusCounts(details)) {
    if (count <= 0) {
      continue;
    }
    const { i18nKey } = getOtaJobStatusPresentation(statusKey);
    const name = i18nKey ? t(i18nKey, statusKey) : statusKey;
    config[statusKey] = {
      label: t("details.nodes.statusSummary.legendItem", "{{count}} {{name}}", {
        count,
        name,
      }),
      color: getOtaJobStatusChartColor(statusKey),
    };
  }
  return config;
}
