/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { NodeTypeModelFilterValue } from "@/aws/components/node-filters/node-type-model-filter";
import type { ThingListFilters, ThingOnlineStatus } from "../../nodes.props";
import type { IndexField } from "@/aws/components/advanced-indices-search";

export interface NodesListFiltersPanelProps {
  filters: ThingListFilters;
  searchBoxKey: number;
  advancedSearchFields: IndexField[];
  onSearch: (value: string) => void;
  onSearchClear: () => void;
  onStatusFilterChange: (status: ThingOnlineStatus | null) => void;
  onTypeModelFilterChange: (value: NodeTypeModelFilterValue | null) => void;
  onFirmwareVersionChange: (value: string | null) => void;
  onAdvancedSearch: (query: string) => void;
  onClearAllFilters: () => void;
}
