/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  DataTable,
  InlineError,
} from "@espressif/dashboard-ui-components/components";
import { getOtaJobNodeExecutionsColumns } from "../../_columns";
import { useOtaJobExecutions } from "../../use-ota-job-executions";
import type { OtaJobNodeExecutionsMainContentProps } from "./ota-job-node-executions-main-content.props";

export default function OtaJobNodeExecutionsMainContent({
  jobId,
  onSelectNode,
}: OtaJobNodeExecutionsMainContentProps) {
  const { t } = useTranslation(["ota-jobs", "common"]);
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
  } = useOtaJobExecutions(jobId);

  const columns = useMemo(() => getOtaJobNodeExecutionsColumns(t), [t]);

  if (error) {
    return (
      <InlineError
        title={t(
          "details.nodes.executions.error.title",
          "Failed to load node executions",
        )}
      >
        {error instanceof Error
          ? error.message
          : t(
              "details.nodes.executions.error.description",
              "An unexpected error occurred while loading node executions. Please try again later.",
            )}
      </InlineError>
    );
  }

  return (
    <DataTable
      columns={columns}
      data={rows}
      onRowClick={(row) => onSelectNode(row.thingName)}
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
      noResultsHeading={t(
        "details.nodes.executions.noResultsHeading",
        "No node executions",
      )}
      noResultsDescription={t(
        "details.nodes.executions.noResultsDescription",
        "This job has no node executions yet.",
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
