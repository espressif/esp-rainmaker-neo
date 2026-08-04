/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { Job } from "@aws-sdk/client-iot";

export interface OtaJobActivityCardProps {
  job: Job;
}
