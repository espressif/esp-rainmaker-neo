/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  Button,
  ButtonGroup,
  SearchBox,
} from "@espressif/dashboard-ui-components/components";
import {
  AdvancedIndicesSearch,
  AdvancedIndicesSearchTrigger,
} from "@/aws/components/advanced-indices-search";
import { FirmwareVersionFilter } from "@/aws/components/node-filters/firmware-version-filter";
import { NodeTypeModelFilter } from "@/aws/components/node-filters/node-type-model-filter";
import { hasActiveThingFilters } from "../../nodes.props";
import { ThingStatusFilter } from "../thing-status-filter/thing-status-filter";
import type { NodesListFiltersPanelProps } from "./nodes-list-filters-panel.props";
import { SearchCode, X } from "lucide-react";

export function NodesListFiltersPanel({
  filters,
  searchBoxKey,
  advancedSearchFields,
  onSearch,
  onSearchClear,
  onStatusFilterChange,
  onTypeModelFilterChange,
  onFirmwareVersionChange,
  onAdvancedSearch,
  onClearAllFilters,
}: NodesListFiltersPanelProps) {
  const { t } = useTranslation(["nodes", "common"]);

  const hasActiveFilters = useMemo(
    () => hasActiveThingFilters(filters),
    [filters],
  );

  return (
    <div className="flex w-full">
      <div className="flex items-center gap-5 flex-1">
        <div className="w-xs">
          <SearchBox
            key={searchBoxKey}
            placeholder={t("searchByNodeId", "Search by Node ID")}
            onSearch={onSearch}
            onClear={onSearchClear}
            className="font-normal"
            size="sm"
          />
        </div>

        <ThingStatusFilter
          value={filters.status ?? null}
          onChange={onStatusFilterChange}
        />

        <NodeTypeModelFilter
          value={filters.typeModel ?? null}
          onChange={onTypeModelFilterChange}
        />

        <FirmwareVersionFilter
          value={filters.fwVersion ?? null}
          onChange={onFirmwareVersionChange}
        />

        <AdvancedIndicesSearch
          fields={advancedSearchFields}
          query={filters.advancedSearchQuery}
          onSearch={onAdvancedSearch}
        >
          <ButtonGroup className="border rounded-lg items-center h-8">
            <AdvancedIndicesSearchTrigger>
              <Button
                variant="ghost"
                color="gray"
                fullWidth={false}
                startIcon={<SearchCode />}
                size="sm"
                usePrimaryColorOnHover
              >
                {t("common:advancedSearch", "Advanced Search")}
              </Button>
            </AdvancedIndicesSearchTrigger>
            <Button
              variant="ghost"
              size="icon"
              color="error"
              fullWidth={false}
              hideRingOnHover
              className="text-destructive hover:text-destructive h-8 p-0"
              disabled={!filters.advancedSearchQuery}
              onClick={() => onAdvancedSearch("")}
              tooltip={t("advancedSearch.clear", "Clear advanced search")}
            >
              <X />
            </Button>
          </ButtonGroup>
        </AdvancedIndicesSearch>
      </div>

      <div className="flex items-center justify-end gap-0 shrink-0">
        {hasActiveFilters ? (
          <Button
            type="button"
            variant="link"
            color="error"
            onClick={onClearAllFilters}
            fullWidth={false}
            className="text-sm font-normal"
            size="sm"
          >
            {t("clearAllFilters", "Clear all filters")}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
