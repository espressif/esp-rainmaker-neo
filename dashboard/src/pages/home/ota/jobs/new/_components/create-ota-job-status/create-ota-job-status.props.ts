/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type {
  CreateOtaJobResult,
  CreateOtaJobStatus,
} from "../../_hooks/use-create-ota-job-orchestration";

export interface CreateOtaJobStatusProps {
  status: CreateOtaJobStatus;
  /** Present in the success state; identifies the created job for navigation. */
  result?: CreateOtaJobResult | null;
  /** Present in the failure state; specific reason when detectable. */
  errorMessage?: string;
  onBackToJobs: () => void;
  onViewJobDetails: () => void;
  /** Closes the dialog and returns to the form (values preserved) to fix and resubmit. */
  onEditAndRetry: () => void;
}
