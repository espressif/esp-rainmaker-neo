/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { RegistrationJobStatusResponse } from "@/api/node-registration";

export interface RegistrationJobDetailsProps {
  job: RegistrationJobStatusResponse;
  onDownload: (s3Path: string) => void;
}
