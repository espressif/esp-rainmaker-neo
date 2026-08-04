/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface RegistrationJobRowActionsProps {
  certFileS3Path?: string;
  onDownload: (s3Path: string) => void;
}
