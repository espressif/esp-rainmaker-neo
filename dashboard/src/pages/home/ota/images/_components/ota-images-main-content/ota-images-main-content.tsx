/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  DataTable,
  FullSizeError,
} from "@espressif/dashboard-ui-components/components";
import { getOtaImagesColumns } from "../../_columns";
import type { OtaImagesMainContentProps } from "./ota-images-main-content.props";

export function OtaImagesMainContent({
  rows,
  error,
  isFetching,
  pagination,
  hasNextPage,
  hasPrevPage,
  hasActiveSearch,
  searchTerm,
  onNextPage,
  onPrevPage,
  onPageSizeChange,
}: OtaImagesMainContentProps) {
  const { t } = useTranslation(["ota-images", "common"]);

  const columns = useMemo(() => getOtaImagesColumns(t), [t]);

  // Rendered inside the page body rather than replacing the page, so the search box in
  // the header stays available to clear a term that triggered the failure.
  if (error) {
    return (
      <FullSizeError title={t("error.title", "Failed to load OTA images")}>
        {error.message ||
          t(
            "error.description",
            "An unexpected error occurred while loading OTA images. Please try again later."
          )}
      </FullSizeError>
    );
  }

  const noResultsHeading = hasActiveSearch
    ? t("search.noResults", "No matching OTA images")
    : t("noResults", "No OTA images");

  const noResultsDescription = hasActiveSearch
    ? t(
        "search.noResultsDescription",
        'No image name starts with "{{query}}". Search matches the start of the file name and is case-sensitive.',
        { query: searchTerm }
      )
    : t("noResultsDescription", "No OTA images have been uploaded yet.");

  return (
    <DataTable
      columns={columns}
      data={rows}
      tableRowClassName="group"
      isFetching={isFetching}
      pageIndex={pagination.pageIndex}
      hasNextPage={hasNextPage}
      hasPrevPage={hasPrevPage}
      onNextPage={onNextPage}
      onPrevPage={onPrevPage}
      pageSize={pagination.pageSize}
      onPageSizeChange={onPageSizeChange}
      showBorder
      showColumnVisibilitySelector={false}
      noResultsHeading={noResultsHeading}
      noResultsDescription={noResultsDescription}
      pageLabel={t("common:dataTable.page", "Page {{current}}", {
        current: pagination.pageIndex + 1,
      })}
      previousLabel={t("common:dataTable.previous", "Previous")}
      nextLabel={t("common:dataTable.next", "Next")}
      columnVisibilityLabel={t(
        "common:dataTable.columnVisibility.label",
        "Columns"
      )}
      pageSizeOptionLabel={(size) =>
        `${size} ${t("common:dataTable.rowsPerPage", "rows per page")}`
      }
    />
  );
}
