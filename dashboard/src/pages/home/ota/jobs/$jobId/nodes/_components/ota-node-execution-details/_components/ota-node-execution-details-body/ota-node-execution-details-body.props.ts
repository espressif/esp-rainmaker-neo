/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { JobExecution } from "@aws-sdk/client-iot";

export interface OtaNodeExecutionDetailsBodyProps {
  jobId: string;
  thingName: string;
  execution: JobExecution | null;
  isPending: boolean;
  isError: boolean;
}
