/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";

export interface GroupNodesCardShellProps {
  /** Dynamic groups get no "Add nodes" action — membership follows the group's query rules. */
  isDynamic: boolean;
  children: ReactNode;
}
