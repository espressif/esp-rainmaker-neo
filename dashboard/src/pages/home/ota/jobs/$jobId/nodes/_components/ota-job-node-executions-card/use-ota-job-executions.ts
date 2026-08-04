/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTokenPagination } from "@/hooks/use-token-pagination";
import { useOtaJobExecutionsQuery } from "@/api/ota-jobs";
import type { OtaJobNodeExecutionRow } from "./ota-job-node-executions-card.props";

const INITIAL_PAGE_SIZE = 10;

export function useOtaJobExecutions(jobId: string) {
  const {
    state: pagination,
    hasPrevPage,
    handlePageSizeChange,
    goNext,
    goPrev,
  } = useTokenPagination(INITIAL_PAGE_SIZE);

  const { data, error, isFetching } = useOtaJobExecutionsQuery({
    jobId,
    pageSize: pagination.pageSize,
    nextToken: pagination.nextToken,
  });

  const rows: OtaJobNodeExecutionRow[] = (data?.executions ?? []).map(
    (execution) => ({
      thingName: execution.thingName,
      status: execution.status,
      lastUpdatedAt: execution.lastUpdatedAt,
    }),
  );

  const hasNextPage = !!data?.nextToken;

  const handleNextPage = useCallback(() => {
    goNext(data?.nextToken);
  }, [goNext, data?.nextToken]);

  return {
    pagination,
    rows,
    error,
    isFetching,
    hasNextPage,
    hasPrevPage,
    handleNextPage,
    handlePrevPage: goPrev,
    handlePageSizeChange,
  };
}
