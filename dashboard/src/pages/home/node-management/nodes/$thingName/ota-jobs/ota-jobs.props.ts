/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface ThingOtaJobRow {
  jobId: string;
  status?: string;
  queuedAt?: Date;
  startedAt?: Date;
  lastUpdatedAt?: Date;
  executionNumber?: number;
}
