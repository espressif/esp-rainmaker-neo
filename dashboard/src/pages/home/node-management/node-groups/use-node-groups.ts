/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useState } from "react";
import { useTokenPagination } from "@/hooks/use-token-pagination";
import { useNodeGroupsListQuery } from "@/api/node-groups";
import type { NodeGroupRow } from "./node-groups.props";
import type { NodeGroupsSearchValue } from "./_components/node-groups-page-header";

const INITIAL_PAGE_SIZE = 10;

export function useNodeGroups() {
  const {
    state: pagination,
    hasPrevPage,
    handlePageSizeChange,
    goNext,
    goPrev,
    resetPagination,
  } = useTokenPagination(INITIAL_PAGE_SIZE);

  const [searchQuery, setSearchQuery] = useState<NodeGroupsSearchValue | null>(
    null,
  );

  const { data, isLoading, error, isFetching } = useNodeGroupsListQuery({
    pageSize: pagination.pageSize,
    nextToken: pagination.nextToken,
    searchField: searchQuery?.id,
    searchValue: searchQuery?.value,
  });

  const rows: NodeGroupRow[] = (data?.thingGroups ?? []).map((g) => ({
    groupName: g.groupName,
    groupId: g.groupId,
    groupDescription: g.groupDescription,
    parentGroupNames: g.parentGroupNames,
  }));

  const hasNextPage = !!data?.nextToken;

  const handleNextPage = useCallback(() => {
    goNext(data?.nextToken);
  }, [goNext, data?.nextToken]);

  const handleSearch = useCallback(
    (query: NodeGroupsSearchValue | null) => {
      setSearchQuery(query);
      resetPagination();
    },
    [resetPagination],
  );

  return {
    pagination,
    rows,
    isLoading,
    error,
    isFetching,
    hasNextPage,
    hasPrevPage,
    handleNextPage,
    handlePrevPage: goPrev,
    handlePageSizeChange,
    handleSearch,
  };
}
