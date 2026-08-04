/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { CloudDownload } from "lucide-react";
import {
  AnimatedCard,
  Button,
  ContentContainer,
  DataTable,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import { getGroupOtaJobsColumns } from "../../_columns";
import { useGroupOtaJobs } from "../../use-group-ota-jobs";
import type { GroupOtaJobsMainContentProps } from "./group-ota-jobs-main-content.props";

export default function GroupOtaJobsMainContent({
  groupName,
}: GroupOtaJobsMainContentProps) {
  const { t } = useTranslation(["node-groups", "common"]);
  const navigate = useNavigate();

  const {
    pagination,
    rows,
    isLoading,
    isFetching,
    error,
    refetch,
    hasNextPage,
    hasPrevPage,
    handleNextPage,
    handlePrevPage,
    handlePageSizeChange,
  } = useGroupOtaJobs(groupName);

  const columns = useMemo(() => getGroupOtaJobsColumns(t), [t]);

  return (
    <ContentContainer maxWidth="lg" noGutters>
      <SectionCard
        icon={<CloudDownload className="h-5 w-5" />}
        primaryText={t(
          "details.otaJobs.title",
          "OTA jobs for this group",
        )}
        secondaryText={t(
          "details.otaJobs.description",
          "OTA jobs targeting this node group.",
        )}
        color="silver"
        variant="outline"
        allowCollapse={false}
      >
        {error ? (
          <AnimatedCard
            type="errorSpreadOut"
            iconSize={96}
            actions={
              <Button
                type="button"
                variant="ghost"
                size="sm"
                color="primary"
                fullWidth={false}
                onClick={() => void refetch()}
              >
                {t("common:actions.tryAgain", "Try again")}
              </Button>
            }
          >
            {t(
              "details.otaJobs.error.title",
              "Failed to load OTA jobs",
            )}
          </AnimatedCard>
        ) : (
          <DataTable
            columns={columns}
            data={rows}
            isFetching={isLoading || isFetching}
            pageIndex={pagination.pageIndex}
            pageSize={pagination.pageSize}
            hasNextPage={hasNextPage}
            hasPrevPage={hasPrevPage}
            onNextPage={handleNextPage}
            onPrevPage={handlePrevPage}
            onPageSizeChange={handlePageSizeChange}
            onRowClick={(row) =>
              void navigate({
                to: "/home/ota/jobs/$jobId",
                params: { jobId: row.jobId },
              })
            }
            tableRowClassName="group"
            showBorder
            showColumnVisibilitySelector={false}
            noResultsHeading={t(
              "details.otaJobs.noResults.title",
              "No OTA jobs",
            )}
            noResultsDescription={t(
              "details.otaJobs.noResults.description",
              "There are no OTA jobs for this group yet.",
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
        )}
      </SectionCard>
    </ContentContainer>
  );
}
