/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface GvaJsonUploadFieldProps {
  /** Name of the last successfully parsed file, or null. */
  fileName: string | null;
  /** Parse/validation error to show below the picker, or null. */
  fileError: string | null;
  /** Receives the picked files; the parent reads + parses the JSON. */
  onFilesChange: (files: File[]) => void;
}
