/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useSetState } from "react-use";
import type { JobStatus, TargetSelection } from "@aws-sdk/client-iot";
import { useTokenPagination } from "@/hooks/use-token-pagination";
import { useOtaJobsListQuery } from "@/api/ota-jobs";
import {
  CLEARED_OTA_JOB_FILTERS,
  type OtaJobRow,
  type OtaJobsFilters,
} from "./ota-jobs.props";

const INITIAL_PAGE_SIZE = 10;

export function useOtaJobs() {
  const [filters, setFilters] = useSetState<OtaJobsFilters>({});

  const {
    state: pagination,
    hasPrevPage,
    handlePageSizeChange,
    goNext,
    goPrev,
    resetPagination,
  } = useTokenPagination(INITIAL_PAGE_SIZE);

  const { data, isLoading, error, isFetching } = useOtaJobsListQuery({
    pageSize: pagination.pageSize,
    nextToken: pagination.nextToken,
    status: filters.status,
    targetSelection: filters.targetSelection,
    thingGroupName: filters.groupName,
  });

  const rows: OtaJobRow[] = (data?.jobs ?? []).map((job) => ({
    jobId: job.jobId ?? "",
    jobArn: job.jobArn,
    createdAt: job.createdAt,
    status: job.status,
    targetSelection: job.targetSelection,
  }));

  const hasNextPage = !!data?.nextToken;

  const handleNextPage = useCallback(() => {
    goNext(data?.nextToken);
  }, [goNext, data?.nextToken]);

  const handleStatusChange = useCallback(
    (status: JobStatus | null) => {
      setFilters({ status: status ?? undefined });
      resetPagination();
    },
    [setFilters, resetPagination],
  );

  const handleTargetSelectionChange = useCallback(
    (targetSelection: TargetSelection | null) => {
      setFilters({ targetSelection: targetSelection ?? undefined });
      resetPagination();
    },
    [setFilters, resetPagination],
  );

  const handleGroupChange = useCallback(
    (groupName: string | undefined) => {
      setFilters({ groupName });
      resetPagination();
    },
    [setFilters, resetPagination],
  );

  const handleClearAllFilters = useCallback(() => {
    setFilters(CLEARED_OTA_JOB_FILTERS);
    resetPagination();
  }, [setFilters, resetPagination]);

  return {
    pagination,
    filters,
    rows,
    isLoading,
    error,
    isFetching,
    hasNextPage,
    hasPrevPage,
    handleNextPage,
    handlePrevPage: goPrev,
    handlePageSizeChange,
    handleStatusChange,
    handleTargetSelectionChange,
    handleGroupChange,
    handleClearAllFilters,
  };
}
