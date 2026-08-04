/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export type QuotaProgressColor = "success" | "warning" | "error";

export function getQuotaProgressColor(value: number): QuotaProgressColor {
  if (value > 90) {return "error";}
  if (value >= 50) {return "warning";}
  return "success";
}

export interface ClaimedQuotaMetrics {
  progressValue: number;
  progressRaw: number;
  percentRounded: number;
  color: QuotaProgressColor;
  total: number;
  quota: number;
}

export function getClaimedQuotaMetrics(
  total: number,
  quota: number,
): ClaimedQuotaMetrics | null {
  if (!Number.isFinite(total) || !Number.isFinite(quota) || quota <= 0) {
    return null;
  }

  const progressRaw = (total / quota) * 100;
  const progressValue = Math.min(100, Math.max(0, progressRaw));
  const percentRounded = Math.min(100, Math.round(progressRaw));
  const color = getQuotaProgressColor(progressRaw);

  return {
    progressValue,
    progressRaw,
    percentRounded,
    color,
    total,
    quota,
  };
}
