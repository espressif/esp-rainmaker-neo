/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { RegistrationJobStatusResponse } from "@/api/node-registration";

export interface RegistrationJobDetailsProps {
  job: RegistrationJobStatusResponse;
  onDownload: (s3Path: string) => void;
  /**
   * Namespace for detail-sheet copy (`unknownFile`, `details.*`).
   * Defaults to `register`; pass `update` when rendering update jobs.
   */
  i18nNamespace?: string;
  /**
   * Shown when `job.job_type` is absent. Defaults to `register`.
   */
  defaultJobType?: string;
}
