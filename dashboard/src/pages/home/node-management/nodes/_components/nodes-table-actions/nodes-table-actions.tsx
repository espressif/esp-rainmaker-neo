/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { NodesQuotaDetails } from "../nodes-quota-details/nodes-quota-details";

export function NodesTableActions() {
  return (
    <div className="flex items-center gap-5">
      <NodesQuotaDetails />
    </div>
  );
}
