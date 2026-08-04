/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useEffect, useMemo } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { searchThingGroups } from "@/aws/services/thing-group.service";
import { useAuthStore } from "@/stores/auth.store";
import { useDebouncedSearch } from "./use-debounced-search";

const PAGE_SIZE = 15;

interface UseThingGroupSearchArgs {
  enabled: boolean;
  onError?: (error: Error) => void;
  /** Only return parent-less (top-level) groups — used by the register flow. */
  topLevelOnly?: boolean;
  /** Scope the search to children of this group — used for subgroup selection. */
  parentGroupName?: string;
}

/**
 * Single source of truth for group lookups behind every group selector.
 * Paginated, debounced and auth-gated; `topLevelOnly`/`parentGroupName` shape
 * the query so the same hook serves top-level lists and subgroup lists.
 */
export function useThingGroupSearch({
  enabled,
  onError,
  topLevelOnly = false,
  parentGroupName,
}: UseThingGroupSearchArgs) {
  const credentials = useAuthStore((s) => s.credentials);
  const { debouncedSearch, setSearchInput } = useDebouncedSearch();

  const nameFilter = debouncedSearch
    ? `thingGroupName:*${debouncedSearch}*`
    : "";
  const parentFilter = parentGroupName
    ? `parentGroupNames:${parentGroupName}`
    : "";
  const queryString =
    [parentFilter, nameFilter].filter(Boolean).join(" AND ") || "*";

  const query = useInfiniteQuery({
    queryKey: [
      "iot",
      "thing-group-search",
      { topLevelOnly, parentGroupName: parentGroupName ?? null, debouncedSearch },
    ],
    queryFn: ({ pageParam }) =>
      searchThingGroups({
        queryString,
        maxResults: PAGE_SIZE,
        nextToken: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextToken ?? undefined,
    enabled: !!credentials && enabled,
  });

  useEffect(() => {
    if (query.error && onError) {
      onError(
        query.error instanceof Error
          ? query.error
          : new Error(String(query.error)),
      );
    }
  }, [query.error, onError]);

  const groups = useMemo(() => {
    const all = (query.data?.pages ?? []).flatMap((page) => page.thingGroups);
    return topLevelOnly
      ? all.filter((g) => g.parentGroupNames.length === 0)
      : all;
  }, [query.data, topLevelOnly]);

  const loadMore = useCallback(() => {
    if (query.hasNextPage && !query.isFetchingNextPage) {
      void query.fetchNextPage();
    }
  }, [query]);

  return {
    groups,
    setSearchInput,
    isLoading: query.isFetching,
    hasMore: !!query.hasNextPage,
    loadMore,
  };
}
