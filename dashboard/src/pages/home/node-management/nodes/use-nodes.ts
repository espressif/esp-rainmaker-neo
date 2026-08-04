/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSetState } from "react-use";
import { useAuthStore } from "@/stores/auth.store";
import { listThings, searchThings } from "@/aws/services/thing.service";
import { useTokenPagination } from "@/hooks/use-token-pagination";
import { extractIparamsFields } from "./iparams-fields";
import { buildThingsSearchQuery } from "./_utils/build-things-search-query";
import type { ThingRow } from "./_columns/nodes-columns";
import {
  CLEARED_THING_LIST_FILTERS,
  type ThingListFilters,
  type ThingOnlineStatus,
  type ThingTypeModelFilterValue,
} from "./nodes.props";

const POPOVER_FILTER_CLEAR: Partial<ThingListFilters> = {
  thingName: undefined,
};

const SEARCH_CLEAR: Partial<ThingListFilters> = {
  status: undefined,
  typeModel: undefined,
  fwVersion: undefined,
};

export function useNodes(enabled: boolean) {
  const credentials = useAuthStore((s) => s.credentials);
  const [filters, setFilters] = useSetState<ThingListFilters>({});
  const [searchBoxKey, setSearchBoxKey] = useState(0);
  const {
    state: pagination,
    hasPrevPage,
    handlePageSizeChange,
    goNext,
    goPrev,
    resetPagination,
  } = useTokenPagination(10);

  const queryString = buildThingsSearchQuery(filters);

  const { data, isLoading, error, isFetching } = useQuery({
    queryKey: [
      "iot",
      "things",
      pagination.nextToken,
      pagination.pageSize,
      filters,
    ],
    queryFn: async () => {
      try {
        const response = await searchThings({
          queryString,
          maxResults: pagination.pageSize,
          indexName: "AWS_Things",
          nextToken: pagination.nextToken,
        });

        return {
          things: response.things.map((thing) => {
            const fields = extractIparamsFields(thing.shadow);
            const connectivity = thing.connectivity;
            if (connectivity?.timestamp != null && fields.lastSeen == null) {
              fields.lastSeen = Math.floor(connectivity.timestamp / 1000);
            }
            return {
              thingId: thing.thingId ?? thing.thingName ?? "",
              thingName: fields.displayName,
              awsThingName: thing.thingName ?? "",
              online: fields.online,
              deviceType: fields.deviceType,
              deviceModel: fields.deviceModel,
              fwVersion: fields.fwVersion,
              lastSeen: fields.lastSeen,
            };
          }),
          nextToken: response.nextToken,
          usedFallback: false,
        };
      } catch {
        const response = await listThings({
          maxResults: pagination.pageSize,
          nextToken: pagination.nextToken,
        });

        return {
          things: response.things.map((thing) => ({
            thingId: thing.thingName ?? "",
            thingName: null,
            awsThingName: thing.thingName ?? "",
            online: null,
            deviceType: null,
            deviceModel: null,
            fwVersion: null,
            lastSeen: null,
          })),
          nextToken: response.nextToken,
          usedFallback: true,
        };
      }
    },
    enabled: !!credentials && enabled,
    placeholderData: (prev) => prev,
  });

  const things: ThingRow[] = data?.things ?? [];
  const hasNextPage = !!data?.nextToken;

  const applyFilterChange = useCallback(
    (patch: Partial<ThingListFilters>) => {
      setFilters(patch);
      resetPagination();
    },
    [setFilters, resetPagination],
  );

  const handleSearch = useCallback(
    (value: string) => {
      setFilters({
        ...CLEARED_THING_LIST_FILTERS,
        ...SEARCH_CLEAR,
        thingName: value || undefined,
      });
      resetPagination();
    },
    [setFilters, resetPagination],
  );

  const handleSearchClear = useCallback(() => {
    applyFilterChange({ thingName: undefined });
  }, [applyFilterChange]);

  const handleStatusChange = useCallback(
    (status: ThingOnlineStatus | null) => {
      applyFilterChange({
        ...POPOVER_FILTER_CLEAR,
        status: status ?? undefined,
      });
    },
    [applyFilterChange],
  );

  const handleTypeModelChange = useCallback(
    (value: ThingTypeModelFilterValue | null) => {
      applyFilterChange({
        ...POPOVER_FILTER_CLEAR,
        typeModel: value ?? undefined,
      });
    },
    [applyFilterChange],
  );

  const handleFirmwareVersionChange = useCallback(
    (value: string | null) => {
      applyFilterChange({
        ...POPOVER_FILTER_CLEAR,
        fwVersion: value ?? undefined,
      });
    },
    [applyFilterChange],
  );

  const handleAdvancedSearch = useCallback(
    (query: string) => {
      applyFilterChange({ advancedSearchQuery: query || undefined });
    },
    [applyFilterChange],
  );

  const handleClearAllFilters = useCallback(() => {
    setFilters(CLEARED_THING_LIST_FILTERS);
    resetPagination();
    setSearchBoxKey((key) => key + 1);
  }, [setFilters, resetPagination]);

  const handleNextPage = useCallback(() => {
    goNext(data?.nextToken);
  }, [goNext, data?.nextToken]);

  return {
    filters,
    searchBoxKey,
    pagination,
    things,
    isLoading,
    error,
    isFetching,
    hasNextPage,
    hasPrevPage,
    usedFallback: data?.usedFallback ?? false,
    handleSearch,
    handleSearchClear,
    handleStatusChange,
    handleTypeModelChange,
    handleFirmwareVersionChange,
    handleAdvancedSearch,
    handleClearAllFilters,
    handlePageSizeChange,
    handleNextPage,
    handlePrevPage: goPrev,
  };
}
