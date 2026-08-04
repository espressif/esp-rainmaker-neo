/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { JobProcessDetails } from "@aws-sdk/client-iot";
import { getOtaJobStatusCounts } from "@/config/ota-job-status.config";

export interface OtaJobCompletionTotals {
  succeeded: number;
  total: number;
}

/** AWS exposes no `total`; it is the sum of every per-status count. */
export function getOtaJobCompletionTotals(
  details: JobProcessDetails | undefined,
): OtaJobCompletionTotals {
  const counts = getOtaJobStatusCounts(details);
  const total = counts.reduce((sum, { count }) => sum + count, 0);
  const succeeded = details?.numberOfSucceededThings ?? 0;
  return { succeeded, total };
}

export function getOtaJobCompletionPercent(
  details: JobProcessDetails | undefined,
): number {
  const { succeeded, total } = getOtaJobCompletionTotals(details);
  if (total <= 0) {
    return 0;
  }
  return Math.round((succeeded / total) * 100);
}
