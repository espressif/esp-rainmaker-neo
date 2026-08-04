/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LucideIcon } from "lucide-react";
import { CircleDashed, HelpCircle, MessageSquareDot, ShieldAlert } from "lucide-react";
import type { Color } from "@espressif/dashboard-ui-components";

/**
 * Whether the account still needs destination verification at all. `loading` is
 * deliberately absent: it is a fetch state, not a status, and the card renders no
 * badge until the status is known.
 */
export type SmsSandboxStatus = "sandbox" | "production" | "unknown";

export interface SmsSandboxStatusPresentation {
  Icon: LucideIcon;
  color: Color;
  /** i18n key under the `post-deployment` namespace. Empty signals an unmapped status. */
  i18nKey: string;
  labelFallback: string;
}

export const SMS_SANDBOX_STATUS_PRESENTATION: Record<
  SmsSandboxStatus,
  SmsSandboxStatusPresentation
> = {
  sandbox: {
    Icon: ShieldAlert,
    color: "warning",
    i18nKey: "post-deployment:smsSandbox.status.sandbox",
    labelFallback: "In sandbox",
  },
  production: {
    Icon: MessageSquareDot,
    color: "success",
    i18nKey: "post-deployment:smsSandbox.status.production",
    labelFallback: "Production access enabled",
  },
  unknown: {
    Icon: HelpCircle,
    color: "gray",
    i18nKey: "post-deployment:smsSandbox.status.unknown",
    labelFallback: "Status unknown",
  },
};

export const SMS_SANDBOX_STATUS_IDS: readonly SmsSandboxStatus[] = [
  "sandbox",
  "production",
  "unknown",
];

const UNKNOWN_STATUS_PRESENTATION: SmsSandboxStatusPresentation = {
  Icon: CircleDashed,
  color: "gray",
  i18nKey: "",
  labelFallback: "",
};

export function isSmsSandboxStatus(
  status: string | null | undefined,
): status is SmsSandboxStatus {
  return !!status && status in SMS_SANDBOX_STATUS_PRESENTATION;
}

export function getSmsSandboxStatusPresentation(
  status: string | null | undefined,
): SmsSandboxStatusPresentation {
  if (isSmsSandboxStatus(status)) {
    return SMS_SANDBOX_STATUS_PRESENTATION[status];
  }
  return UNKNOWN_STATUS_PRESENTATION;
}
