/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface RegistrationJobFileNameCellProps {
  requestId: string;
  certFileS3Path?: string;
  failedCount: number;
  /**
   * Namespace for the `unknownFile` fallback. Defaults to `register`;
   * pass `update` when rendering update jobs.
   */
  i18nNamespace?: string;
}
