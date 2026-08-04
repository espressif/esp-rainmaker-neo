/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { NodesListFiltersPanel } from "../nodes-list-filters-panel/nodes-list-filters-panel";
import type { NodesPageHeaderProps } from "./nodes-page-header.props";

export default function NodesPageHeader({ ...props }: NodesPageHeaderProps) {
  return (
    <div>
      <div className="flex items-center justify-between gap-4 p-5 bg-accent/10 w-full">
        <NodesListFiltersPanel {...props} />
      </div>
    </div>
  );
}
