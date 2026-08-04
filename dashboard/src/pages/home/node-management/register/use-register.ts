/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useState } from "react";
import { useTokenPagination } from "@/hooks/use-token-pagination";
import { useRegistrationJobs } from "@/api/node-registration";
import type { RegistrationJobRow } from "./register.props";
import type { RegistrationJobStatus } from "@/config/registration-job-status.config";

const INITIAL_PAGE_SIZE = 20;

export function useRegister() {
  const [statusFilter, setStatusFilter] = useState<RegistrationJobStatus | null>(
    null,
  );

  const {
    state: pagination,
    hasPrevPage,
    handlePageSizeChange,
    goNext,
    goPrev,
    resetPagination,
  } = useTokenPagination(INITIAL_PAGE_SIZE);

  const { data, isLoading, error, isFetching } = useRegistrationJobs({
    pageSize: pagination.pageSize,
    startKey: pagination.nextToken,
    status: statusFilter ?? undefined,
  });

  const rows: RegistrationJobRow[] = (data?.jobs ?? []).map((job) => ({
    requestId: job.request_id,
    status: job.status,
    totalNodes: job.total_nodes ?? 0,
    successCount: job.success_count ?? 0,
    failedCount: job.failed_count ?? 0,
    lastUpdatedAt: job.last_updated_at,
    certFileS3Path: job.cert_file_s3_path,
    raw: job,
  }));

  const hasNextPage = !!data?.next_key;

  const handleNextPage = useCallback(() => {
    goNext(data?.next_key);
  }, [goNext, data?.next_key]);

  const handleStatusChange = useCallback(
    (next: RegistrationJobStatus | null) => {
      setStatusFilter(next);
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
    statusFilter,
    handleStatusChange,
  };
}
