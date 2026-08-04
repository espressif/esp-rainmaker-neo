/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LucideIcon } from "lucide-react";
import { CircleDashed, Clock, PhoneCall } from "lucide-react";
import type { SMSSandboxPhoneNumberVerificationStatus } from "@aws-sdk/client-sns";
import type { Color } from "@espressif/dashboard-ui-components";

/**
 * Verification state of one SMS sandbox destination number. Derived from the SNS
 * SDK so a new upstream status becomes a compile error here rather than an
 * unstyled pill in the table.
 */
export type SandboxNumberStatus = SMSSandboxPhoneNumberVerificationStatus;

export interface SandboxNumberStatusPresentation {
  Icon: LucideIcon;
  color: Color;
  /** i18n key under the `post-deployment` namespace. Empty signals an unmapped status. */
  i18nKey: string;
  labelFallback: string;
}

export const SANDBOX_NUMBER_STATUS_PRESENTATION: Record<
  SandboxNumberStatus,
  SandboxNumberStatusPresentation
> = {
  Verified: {
    Icon: PhoneCall,
    color: "success",
    i18nKey: "post-deployment:smsSandbox.numberStatus.verified",
    labelFallback: "Verified",
  },
  Pending: {
    Icon: Clock,
    color: "warning",
    i18nKey: "post-deployment:smsSandbox.numberStatus.pending",
    labelFallback: "Pending",
  },
};

export const SANDBOX_NUMBER_STATUS_IDS: readonly SandboxNumberStatus[] = [
  "Verified",
  "Pending",
];

const UNKNOWN_STATUS_PRESENTATION: SandboxNumberStatusPresentation = {
  Icon: CircleDashed,
  color: "gray",
  i18nKey: "",
  labelFallback: "",
};

export function isSandboxNumberStatus(
  status: string | null | undefined,
): status is SandboxNumberStatus {
  return !!status && status in SANDBOX_NUMBER_STATUS_PRESENTATION;
}

export function getSandboxNumberStatusPresentation(
  status: string | null | undefined,
): SandboxNumberStatusPresentation {
  if (isSandboxNumberStatus(status)) {
    return SANDBOX_NUMBER_STATUS_PRESENTATION[status];
  }
  return UNKNOWN_STATUS_PRESENTATION;
}

/** Only unverified numbers can be resent a code or verified. */
export function isSandboxNumberVerified(
  status: string | null | undefined,
): boolean {
  return status === "Verified";
}
