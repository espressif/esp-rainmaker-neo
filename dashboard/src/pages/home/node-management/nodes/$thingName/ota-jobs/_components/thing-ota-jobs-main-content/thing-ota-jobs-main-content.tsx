/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  DataTable,
  InlineError,
} from "@espressif/dashboard-ui-components/components";
import { getThingOtaJobsColumns } from "../../_columns";
import { useThingOtaJobs } from "../../use-thing-ota-jobs";
import type { ThingOtaJobsMainContentProps } from "./thing-ota-jobs-main-content.props";

export default function ThingOtaJobsMainContent({
  thingName,
}: ThingOtaJobsMainContentProps) {
  const { t } = useTranslation(["nodes", "common"]);
  const navigate = useNavigate();
  const {
    pagination,
    rows,
    error,
    isFetching,
    hasNextPage,
    hasPrevPage,
    handleNextPage,
    handlePrevPage,
    handlePageSizeChange,
  } = useThingOtaJobs(thingName);

  const columns = useMemo(() => getThingOtaJobsColumns(t), [t]);

  if (error) {
    return (
      <InlineError title={t("details.otaJobs.error.title", "Failed to load OTA jobs")}>
        {error instanceof Error
          ? error.message
          : t(
              "details.otaJobs.error.description",
              "An unexpected error occurred while loading OTA jobs. Please try again later.",
            )}
      </InlineError>
    );
  }

  return (
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
      noResultsHeading={t("details.otaJobs.noResultsHeading", "No OTA jobs")}
      noResultsDescription={t(
        "details.otaJobs.noResultsDescription",
        "This node has no OTA jobs yet.",
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
  );
}
