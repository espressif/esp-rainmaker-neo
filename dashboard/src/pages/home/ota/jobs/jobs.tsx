/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useNavigate, useLocation, Outlet } from "@tanstack/react-router";
import {
  DataTable,
  FullSizeError,
  PageContainer,
  PageContainerSkeleton,
} from "@espressif/dashboard-ui-components/components";
import { useTranslation } from "react-i18next";
import { getOtaJobsColumns } from "./_columns";
import { OtaJobsPageHeader } from "./_components/ota-jobs-page-header";
import { useOtaJobs } from "./use-ota-jobs";

const OTA_JOBS_ROOT_PATHS = new Set(["/home/ota/jobs", "/home/ota/jobs/"]);

function OtaJobs() {
  const { t } = useTranslation(["ota-jobs", "common"]);
  const navigate = useNavigate();
  const location = useLocation();
  const isChildRoute = !OTA_JOBS_ROOT_PATHS.has(location.pathname);

  const {
    pagination,
    filters,
    rows,
    isLoading,
    error,
    isFetching,
    hasNextPage,
    hasPrevPage,
    handleNextPage,
    handlePrevPage,
    handlePageSizeChange,
    handleStatusChange,
    handleTargetSelectionChange,
    handleGroupChange,
    handleClearAllFilters,
  } = useOtaJobs();

  const columns = useMemo(() => getOtaJobsColumns(t), [t]);

  if (isChildRoute) {
    return <Outlet />;
  }

  if (isLoading) {
    return (
      <PageContainerSkeleton maxWidth="xl" showHeader showActions={false} />
    );
  }

  if (error) {
    return (
      <FullSizeError title={t("error.title", "Failed to load OTA jobs")}>
        {error instanceof Error ? error.message : t("error.description", "An unexpected error occurred while loading OTA jobs. Please try again later.")}
      </FullSizeError>
    );
  }

  return (
    <PageContainer
      noGutters
      className="p-0"
      elevateHeading
      heading={
        <OtaJobsPageHeader
          filters={filters}
          onStatusChange={handleStatusChange}
          onTargetSelectionChange={handleTargetSelectionChange}
          onGroupChange={handleGroupChange}
          onClearAllFilters={handleClearAllFilters}
          onCreateClick={() => void navigate({ to: "/home/ota/jobs/new" })}
        />
      }
    >
      <div className="px-5 pb-5">
        <DataTable
          columns={columns}
          data={rows}
          onRowClick={(row) =>
            void navigate({
              to: "/home/ota/jobs/$jobId",
              params: { jobId: row.jobId },
            })
          }
          tableRowClassName="group"
          isFetching={isFetching}
          pageIndex={pagination.pageIndex}
          hasNextPage={hasNextPage}
          hasPrevPage={hasPrevPage}
          onNextPage={handleNextPage}
          onPrevPage={handlePrevPage}
          pageSize={pagination.pageSize}
          onPageSizeChange={handlePageSizeChange}
          showBorder
          showColumnVisibilitySelector={false}
          noResultsHeading={t("noResults", "No results")}
          noResultsDescription={t("noResultsDescription", "No OTA jobs found.")}
          pageLabel={t("common:dataTable.page", "Page {{current}}", {
            current: pagination.pageIndex + 1,
          })}
          previousLabel={t("common:dataTable.previous", "Previous")}
          nextLabel={t("common:dataTable.next", "Next")}
          columnVisibilityLabel={t("common:dataTable.columnVisibility.label", "Columns")}
          pageSizeOptionLabel={(size) =>
            `${size} ${t("common:dataTable.rowsPerPage", "rows per page")}`
          }
        />
      </div>
    </PageContainer>
  );
}

export default OtaJobs;
