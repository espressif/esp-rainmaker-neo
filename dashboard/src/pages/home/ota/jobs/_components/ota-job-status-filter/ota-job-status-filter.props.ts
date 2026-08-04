/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { JobStatus } from "@aws-sdk/client-iot";

export interface OtaJobStatusFilterProps {
  value: JobStatus | null;
  onChange: (status: JobStatus | null) => void;
}
