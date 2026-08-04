/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface RegistrationJobFileNameCellProps {
  requestId: string;
  certFileS3Path?: string;
  failedCount: number;
}
