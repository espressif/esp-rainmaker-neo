/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";
import type { AwsThingRow } from "./use-aws-things-list";

export interface AwsThingsListProps {
  maxResults?: number;
  actions?: (row: AwsThingRow) => ReactNode;
}
