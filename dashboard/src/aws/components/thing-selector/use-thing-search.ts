/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useEffect, useMemo } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { searchThings } from "@/aws/services/thing.service";
import { useAuthStore } from "@/stores/auth.store";
import { useDebouncedSearch } from "./use-debounced-search";

const PAGE_SIZE = 15;

interface UseThingSearchArgs {
  enabled: boolean;
  onError?: (error: Error) => void;
}

/**
 * Single source of truth for node/thing lookups behind the thing selector.
 * Paginated, debounced and auth-gated — mirrors `useThingGroupSearch` so both
 * selectors share the same fetch behaviour.
 */
export function useThingSearch({ enabled, onError }: UseThingSearchArgs) {
  const credentials = useAuthStore((s) => s.credentials);
  const { debouncedSearch, setSearchInput } = useDebouncedSearch();

  const queryString = debouncedSearch ? `thingName:*${debouncedSearch}*` : "*";

  const query = useInfiniteQuery({
    queryKey: ["iot", "thing-search", debouncedSearch],
    queryFn: ({ pageParam }) =>
      searchThings({
        indexName: "AWS_Things",
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

  const things = useMemo(
    () =>
      (query.data?.pages ?? [])
        .flatMap((page) => page.things)
        .filter((thing) => !!thing.thingName),
    [query.data],
  );

  const loadMore = useCallback(() => {
    if (query.hasNextPage && !query.isFetchingNextPage) {
      void query.fetchNextPage();
    }
  }, [query]);

  return {
    things,
    setSearchInput,
    isLoading: query.isFetching,
    hasMore: !!query.hasNextPage,
    loadMore,
  };
}
