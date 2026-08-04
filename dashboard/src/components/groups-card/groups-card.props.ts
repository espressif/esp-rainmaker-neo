/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";

export interface GroupsCardProps {
  groupNames: string[];
  primaryText: ReactNode;
  emptyText: ReactNode;
}
