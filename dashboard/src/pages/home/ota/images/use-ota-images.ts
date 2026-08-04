/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { otaImagesQueries } from "@/api/ota-images";
import { useTokenPagination } from "@/hooks/use-token-pagination";
import { useAuthStore } from "@/stores/auth.store";
import type { OtaImageRow } from "./ota-images.props";

const INITIAL_PAGE_SIZE = 10;

/**
 * Data layer for the OTA Images table.
 *
 * The listing is split into two queries so the table can render as soon as the
 * (fast) S3 List call returns, then enrich each row independently:
 *  - Query 1 lists objects WITHOUT tags → key/name/size/md5/lastModified.
 *  - Query 2 is one tag fetch per visible row (via `useQueries`) → the
 *    version/type/model/platform columns and the avatar icon fill in as each
 *    per-object tagging call resolves, with no blocking spinner.
 *
 * Search is served by S3's `Prefix`, which only supports a case-sensitive
 * "starts with" match on the image name.
 */
export function useOtaImages() {
  const credentials = useAuthStore((s) => s.credentials);
  const [searchTerm, setSearchTerm] = useState("");
  const {
    state: pagination,
    hasPrevPage,
    handlePageSizeChange,
    goNext,
    goPrev,
    resetPagination,
  } = useTokenPagination(INITIAL_PAGE_SIZE);

  const trimmedSearch = searchTerm.trim();

  const { data, isLoading, error, isFetching } = useQuery({
    ...otaImagesQueries.firmwareFilesList({
      maxKeys: pagination.pageSize,
      continuationToken: pagination.nextToken,
      namePrefix: trimmedSearch || undefined,
    }),
    enabled: !!credentials,
  });

  const baseRows = data?.files ?? [];

  const tagQueries = useQueries({
    queries: baseRows.map((row) => ({
      ...otaImagesQueries.firmwareTags(row.key),
      enabled: !!credentials,
    })),
  });

  const rows: OtaImageRow[] = baseRows.map((row, index) => {
    const tags = tagQueries[index]?.data;
    return tags ? { ...row, ...tags } : row;
  });

  const hasNextPage = !!data?.nextToken;

  const handleNextPage = useCallback(() => {
    goNext(data?.nextToken);
  }, [goNext, data?.nextToken]);

  // A continuation token is only valid for the prefix that produced it, so every
  // change to the search term has to restart the listing from the first page.
  const handleSearch = useCallback(
    (value: string) => {
      setSearchTerm(value);
      resetPagination();
    },
    [resetPagination]
  );

  const handleSearchClear = useCallback(() => {
    setSearchTerm("");
    resetPagination();
  }, [resetPagination]);

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
    searchTerm: trimmedSearch,
    hasActiveSearch: trimmedSearch.length > 0,
    handleSearch,
    handleSearchClear,
  };
}
