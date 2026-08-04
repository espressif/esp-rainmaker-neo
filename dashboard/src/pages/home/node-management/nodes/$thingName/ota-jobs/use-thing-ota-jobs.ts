/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTokenPagination } from "@/hooks/use-token-pagination";
import { useThingOtaJobExecutionsQuery } from "@/api/ota-jobs";
import type { ThingOtaJobRow } from "./ota-jobs.props";

const INITIAL_PAGE_SIZE = 10;

export function useThingOtaJobs(thingName: string) {
  const {
    state: pagination,
    hasPrevPage,
    handlePageSizeChange,
    goNext,
    goPrev,
  } = useTokenPagination(INITIAL_PAGE_SIZE);

  const { data, isLoading, error, isFetching } = useThingOtaJobExecutionsQuery({
    thingName,
    pageSize: pagination.pageSize,
    nextToken: pagination.nextToken,
  });

  const rows: ThingOtaJobRow[] = (data?.executions ?? []).map((exec) => ({
    jobId: exec.jobId,
    status: exec.status,
    queuedAt: exec.queuedAt,
    startedAt: exec.startedAt,
    lastUpdatedAt: exec.lastUpdatedAt,
    executionNumber: exec.executionNumber,
  }));

  const hasNextPage = !!data?.nextToken;

  const handleNextPage = useCallback(() => {
    goNext(data?.nextToken);
  }, [goNext, data?.nextToken]);

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
  };
}
