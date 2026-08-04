/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ThingOnlineStatus } from "../../nodes.props";

export interface ThingStatusFilterProps {
  value: ThingOnlineStatus | null;
  onChange: (status: ThingOnlineStatus | null) => void;
}
