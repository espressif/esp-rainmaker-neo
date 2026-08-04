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
  useDownloadRegistrationCsv,
  type RegistrationJobStatusResponse,
} from "@/api/node-registration";
import { getRegistrationJobsColumns } from "./_columns";
import { RegisterPageHeader } from "./_components/register-page-header";
import { RegistrationJobDetails } from "./_components/registration-job-details";
import { useRegister } from "./use-register";

const REGISTER_ROOT_PATHS = new Set([
  "/home/node-management/register",
  "/home/node-management/register/",
]);

function Register() {
  const { t } = useTranslation(["register", "common"]);
  const navigate = useNavigate();
  const location = useLocation();
  const isChildRoute = !REGISTER_ROOT_PATHS.has(location.pathname);

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
  } = useRegister();

  const [selectedJob, setSelectedJob] =
    useState<RegistrationJobStatusResponse | null>(null);
  const downloadMutation = useDownloadRegistrationCsv();
  const { mutate: downloadCsv } = downloadMutation;

  const handleDownload = useCallback(
    (s3Path: string) => {
      downloadCsv(s3Path);
    },
    [downloadCsv],
  );

  const columns = useMemo(
    () => getRegistrationJobsColumns(t, handleDownload),
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
      <FullSizeError title={t("error.title", "Failed to load registration jobs")}>
        {error instanceof Error
          ? error.message
          : t(
              "error.description",
              "Something went wrong while loading registration jobs.",
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
          <RegisterPageHeader
            statusFilter={statusFilter}
            onStatusFilterChange={handleStatusChange}
            onRegisterClick={() =>
              void navigate({ to: "/home/node-management/register/new" })
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
              "No registration jobs found",
            )}
            noResultsDescription={t(
              "noResultsDescription",
              "Try adjusting the status filter or register new nodes.",
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
          <RegistrationJobDetails job={selectedJob} onDownload={handleDownload} />
        </TableRowDetailSheet>
      )}
    </>
  );
}

export default Register;
