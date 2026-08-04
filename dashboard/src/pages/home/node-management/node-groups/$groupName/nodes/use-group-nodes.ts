/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import {
  useGroupNodesEnrichmentQuery,
  useGroupNodesListQuery,
} from "@/api/node-groups";
import { useTokenPagination } from "@/hooks/use-token-pagination";
import type { ThingRow } from "@/pages/home/node-management/nodes/_columns/nodes-columns";

export function useGroupNodes(groupName: string) {
  const {
    state: pagination,
    hasPrevPage,
    handlePageSizeChange,
    goNext,
    goPrev,
  } = useTokenPagination(10);

  const listQuery = useGroupNodesListQuery({
    groupName,
    pageSize: pagination.pageSize,
    nextToken: pagination.nextToken,
  });

  const thingNames = useMemo(
    () => listQuery.data?.things ?? [],
    [listQuery.data?.things],
  );

  const enrichmentQuery = useGroupNodesEnrichmentQuery(groupName, thingNames);

  const rows: ThingRow[] = useMemo(() => {
    const enrichment = enrichmentQuery.data ?? {};
    return thingNames.map((name) => {
      const fields = enrichment[name];
      return {
        thingId: name,
        thingName: fields?.displayName ?? null,
        awsThingName: name,
        online: fields?.online ?? null,
        deviceType: fields?.deviceType ?? null,
        deviceModel: fields?.deviceModel ?? null,
        fwVersion: fields?.fwVersion ?? null,
        lastSeen: fields?.lastSeen ?? null,
      };
    });
  }, [thingNames, enrichmentQuery.data]);

  const hasNextPage = !!listQuery.data?.nextToken;

  const handleNextPage = () => goNext(listQuery.data?.nextToken);

  return {
    pagination,
    rows,
    isLoading: listQuery.isLoading,
    isFetching: listQuery.isFetching || enrichmentQuery.isFetching,
    error: listQuery.error,
    refetch: listQuery.refetch,
    hasNextPage,
    hasPrevPage,
    handlePageSizeChange,
    handleNextPage,
    handlePrevPage: goPrev,
  };
}
