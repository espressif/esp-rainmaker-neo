/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTokenPagination } from "@/hooks/use-token-pagination";
import { useGroupOtaJobsListQuery } from "@/api/ota-jobs";
import type { GroupOtaJobRow } from "./ota-jobs.props";

const INITIAL_PAGE_SIZE = 10;

export function useGroupOtaJobs(groupName: string) {
  const {
    state: pagination,
    hasPrevPage,
    handlePageSizeChange,
    goNext,
    goPrev,
  } = useTokenPagination(INITIAL_PAGE_SIZE);

  const { data, isLoading, error, isFetching, refetch } =
    useGroupOtaJobsListQuery({
      groupName,
      pageSize: pagination.pageSize,
      nextToken: pagination.nextToken,
    });

  const rows: GroupOtaJobRow[] = (data?.jobs ?? []).map((job) => ({
    jobId: job.jobId ?? "",
    jobArn: job.jobArn,
    createdAt: job.createdAt,
    status: job.status,
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
    refetch,
    hasNextPage,
    hasPrevPage,
    handleNextPage,
    handlePrevPage: goPrev,
    handlePageSizeChange,
  };
}
