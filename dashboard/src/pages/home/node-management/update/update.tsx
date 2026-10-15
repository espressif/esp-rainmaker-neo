/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import {
  Alert,
  DataTable,
  FullSizeError,
  PageContainer,
  PageContainerSkeleton,
} from "@espressif/dashboard-ui-components/components";
import { TableRowDetailSheet } from "@/components/table-row-detail-sheet";
import { getCsvDownloadErrorMessage } from "@/lib/registration-jobs/csv-download-error";
import {
  useDownloadUpdateCsv,
  type UpdateJobStatusResponse,
} from "@/api/node-update";
import { getRegistrationJobsColumns } from "../register/_columns";
import { UpdatePageHeader } from "./_components/update-page-header";
import { RegistrationJobDetails } from "../register/_components/registration-job-details";
import { useUpdate } from "./use-update";

const UPDATE_ROOT_PATHS = new Set([
  "/home/node-management/update",
  "/home/node-management/update/",
]);

function Update() {
  const { t } = useTranslation(["update", "common"]);
  const navigate = useNavigate();
  const location = useLocation();
  const isChildRoute = !UPDATE_ROOT_PATHS.has(location.pathname);

  const {
    pagination,
    rows,
    isLoading,
    error,
    isFetching,
    hasNextPage,
    hasPrevPage,
    handleNextPage,
    handlePrevPage,
    handlePageSizeChange,
    statusFilter,
    handleStatusChange,
  } = useUpdate();

  const [selectedJob, setSelectedJob] =
    useState<UpdateJobStatusResponse | null>(null);
  const downloadMutation = useDownloadUpdateCsv();
  const { mutate: downloadCsv } = downloadMutation;

  const handleDownload = useCallback(
    (s3Path: string) => {
      downloadCsv(s3Path);
    },
    [downloadCsv],
  );

  const columns = useMemo(
    () =>
      getRegistrationJobsColumns(t, handleDownload, {
        i18nNamespace: "update",
      }),
    [t, handleDownload],
  );

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
      <FullSizeError title={t("error.title", "Failed to load update jobs")}>
        {error instanceof Error
          ? error.message
          : t(
              "error.description",
              "Something went wrong while loading update jobs.",
            )}
      </FullSizeError>
    );
  }

  return (
    <>
      <PageContainer
        noGutters
        className="p-0"
        elevateHeading
        heading={
          <UpdatePageHeader
            statusFilter={statusFilter}
            onStatusFilterChange={handleStatusChange}
            onUpdateClick={() =>
              void navigate({ to: "/home/node-management/update/new" })
            }
          />
        }
      >
        <div className="px-5 pb-5 space-y-3">
          {downloadMutation.error && (
            <Alert
              type="error"
              variant="outline"
              title={t("downloadError", "Could not download CSV")}
              description={getCsvDownloadErrorMessage(downloadMutation.error, t)}
            />
          )}
          <DataTable
            columns={columns}
            data={rows}
            onRowClick={(row) => setSelectedJob(row.raw)}
            tableRowClassName="group cursor-pointer"
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
            noResultsHeading={t(
              "noResults",
              "No update jobs found",
            )}
            noResultsDescription={t(
              "noResultsDescription",
              "Try adjusting the status filter or update nodes.",
            )}
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
      {selectedJob && (
        <TableRowDetailSheet
          contentClassName="w-screen max-w-screen-md"
          onOpenChange={(open) => {
            if (!open) {
              setSelectedJob(null);
            }
          }}
        >
          <RegistrationJobDetails
            job={selectedJob}
            onDownload={handleDownload}
            i18nNamespace="update"
            defaultJobType="update"
          />
        </TableRowDetailSheet>
      )}
    </>
  );
}

export default Update;
