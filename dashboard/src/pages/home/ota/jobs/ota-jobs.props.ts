/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { JobStatus, TargetSelection } from "@aws-sdk/client-iot";

export interface OtaJobRow {
  jobId: string;
  jobArn?: string;
  createdAt?: Date;
  status?: string;
  targetSelection?: TargetSelection;
}

export interface OtaJobsFilters {
  status?: JobStatus;
  targetSelection?: TargetSelection;
  groupName?: string;
}

export const CLEARED_OTA_JOB_FILTERS: OtaJobsFilters = {
  status: undefined,
  targetSelection: undefined,
  groupName: undefined,
};

export function hasActiveOtaJobFilters(filters: OtaJobsFilters): boolean {
  if (filters.status) {
    return true;
  }
  if (filters.targetSelection) {
    return true;
  }
  if (filters.groupName) {
    return true;
  }
  return false;
}
