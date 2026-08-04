/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { RegistrationJobStatusResponse } from "@/api/node-registration";

export interface RegistrationJobRow {
  requestId: string;
  status: string;
  totalNodes: number;
  successCount: number;
  failedCount: number;
  lastUpdatedAt?: number;
  certFileS3Path?: string;
  /** Original job payload retained so the details sheet can render every field via DynamicList. */
  raw: RegistrationJobStatusResponse;
}
