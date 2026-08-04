/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LucideIcon } from "lucide-react";
import { CircleDashed, Clock, MailCheck, ShieldAlert, XCircle } from "lucide-react";
import type { Color } from "@espressif/dashboard-ui-components";

/**
 * Whether SES will deliver to unverified recipients. A closed review is not a
 * sandbox to ask out of again — a denied or failed request needs a support case,
 * not another button — so those read differently from a plain sandbox account.
 */
export type SesAccessStatus =
  | "production"
  | "sandbox"
  | "reviewPending"
  | "reviewDenied"
  | "reviewFailed";

export interface SesAccessStatusPresentation {
  Icon: LucideIcon;
  color: Color;
  /** i18n key under the `post-deployment` namespace. Empty signals an unmapped status. */
  i18nKey: string;
  labelFallback: string;
}

export const SES_ACCESS_STATUS_PRESENTATION: Record<
  SesAccessStatus,
  SesAccessStatusPresentation
> = {
  production: {
    Icon: MailCheck,
    color: "success",
    i18nKey: "post-deployment:sesAccessStatus.production",
    labelFallback: "Production access enabled",
  },
  sandbox: {
    Icon: ShieldAlert,
    color: "warning",
    i18nKey: "post-deployment:sesAccessStatus.sandbox",
    labelFallback: "In sandbox",
  },
  reviewPending: {
    Icon: Clock,
    color: "info",
    i18nKey: "post-deployment:sesAccessStatus.reviewPending",
    labelFallback: "In sandbox — production access under review",
  },
  reviewDenied: {
    Icon: XCircle,
    color: "error",
    i18nKey: "post-deployment:sesAccessStatus.reviewDenied",
    labelFallback: "In sandbox — production access denied",
  },
  reviewFailed: {
    Icon: XCircle,
    color: "error",
    i18nKey: "post-deployment:sesAccessStatus.reviewFailed",
    labelFallback: "In sandbox — production access failed",
  },
};

export const SES_ACCESS_STATUS_IDS: readonly SesAccessStatus[] = [
  "production",
  "sandbox",
  "reviewPending",
  "reviewDenied",
  "reviewFailed",
];

const UNKNOWN_STATUS_PRESENTATION: SesAccessStatusPresentation = {
  Icon: CircleDashed,
  color: "gray",
  i18nKey: "",
  labelFallback: "",
};

export function isSesAccessStatus(
  status: string | null | undefined,
): status is SesAccessStatus {
  return !!status && status in SES_ACCESS_STATUS_PRESENTATION;
}

export function getSesAccessStatusPresentation(
  status: string | null | undefined,
): SesAccessStatusPresentation {
  if (isSesAccessStatus(status)) {
    return SES_ACCESS_STATUS_PRESENTATION[status];
  }
  return UNKNOWN_STATUS_PRESENTATION;
}
