/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface RegistrationJobDownloadButtonProps {
  certFileS3Path?: string;
  onDownload: (s3Path: string) => void;
  /** Overrides the generic "Download" label where the file needs naming. */
  label?: string;
  disabled?: boolean;
}
