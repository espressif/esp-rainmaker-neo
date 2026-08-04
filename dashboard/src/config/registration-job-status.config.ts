/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LucideIcon } from "lucide-react";
import { CheckCircle2, Clock, Database, Loader } from "lucide-react";
import type { Color } from "@espressif/dashboard-ui-components";

export type RegistrationJobStatus =
  | "requested"
  | "started"
  | "data_loaded"
  | "completed";

export interface RegistrationJobStatusPresentation {
  Icon: LucideIcon;
  color: Color;
  i18nKey: string;
  labelFallback: string;
}

export const REGISTRATION_JOB_STATUS_PRESENTATION: Record<
  RegistrationJobStatus,
  RegistrationJobStatusPresentation
> = {
  requested: {
    Icon: Clock,
    color: "primary",
    i18nKey: "register:status.requested",
    labelFallback: "Requested",
  },
  started: {
    Icon: Loader,
    color: "warning",
    i18nKey: "register:status.started",
    labelFallback: "Started",
  },
  data_loaded: {
    Icon: Database,
    color: "secondary",
    i18nKey: "register:status.dataLoaded",
    labelFallback: "Data loaded",
  },
  completed: {
    Icon: CheckCircle2,
    color: "success",
    i18nKey: "register:status.completed",
    labelFallback: "Completed",
  },
};

/** Ordered iteration for the status filter dropdown. */
export const REGISTRATION_JOB_STATUS_IDS: readonly RegistrationJobStatus[] = [
  "requested",
  "started",
  "data_loaded",
  "completed",
];

const UNKNOWN_STATUS_PRESENTATION: RegistrationJobStatusPresentation = {
  Icon: Clock,
  color: "gray",
  i18nKey: "",
  labelFallback: "",
};

export function isRegistrationJobStatus(
  status: string | null | undefined,
): status is RegistrationJobStatus {
  return !!status && status in REGISTRATION_JOB_STATUS_PRESENTATION;
}

export function getRegistrationJobStatusPresentation(
  status: string | null | undefined,
): RegistrationJobStatusPresentation {
  if (isRegistrationJobStatus(status)) {
    return REGISTRATION_JOB_STATUS_PRESENTATION[status];
  }
  return UNKNOWN_STATUS_PRESENTATION;
}
