/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type {
  UploadOtaImageResult,
  UploadOtaImageStatus,
} from "../../_hooks/use-upload-ota-image-orchestration";

export interface UploadOtaImageStatusProps {
  status: UploadOtaImageStatus;
  /** Present in the success state; drives the Name / MD5 summary card. */
  result?: UploadOtaImageResult | null;
  /** Present in the failure state; specific reason when detectable. */
  errorMessage?: string;
  onBackToImages: () => void;
  /** Deep-links into the Create OTA Job flow with the uploaded image pre-selected. */
  onCreateOta: () => void;
  /** Closes the dialog and returns to the form (values preserved) to fix and resubmit. */
  onEditAndRetry: () => void;
}
