/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  AnimatedCard,
  Button,
  DataTable,
  SearchBox,
} from "@espressif/dashboard-ui-components/components";
import { getAwsThingsColumns } from "./_columns";
import { useAwsThingsList } from "./use-aws-things-list";
import type { AwsThingsListProps } from "./aws-things-list.props";

const DEFAULT_MAX_RESULTS = 10;

export function AwsThingsList({
  maxResults = DEFAULT_MAX_RESULTS,
  actions,
}: AwsThingsListProps) {
  const { t } = useTranslation(["node-groups", "common"]);

  const {
    pagination,
    rows,
    isLoading,
    isFetching,
    error,
    refetch,
    hasNextPage,
    hasPrevPage,
    handlePageSizeChange,
    handleNextPage,
    handlePrevPage,
    handleSearch,
    handleClearSearch,
  } = useAwsThingsList({ maxResults });

  const columns = useMemo(
    () => getAwsThingsColumns(t, actions),
    [t, actions],
  );

  return (
    <div className="flex flex-col gap-3">
      <SearchBox
        placeholder={t(
          "awsThingsList.searchPlaceholder",
          "Search by node ID",
        )}
        onSearch={handleSearch}
        onClear={handleClearSearch}
        size="sm"
      />
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
          {t("awsThingsList.error.title", "Failed to load nodes")}
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
          tableRowClassName="group"
          showBorder
          showColumnVisibilitySelector={false}
          noResultsHeading={t(
            "awsThingsList.noResults.title",
            "No nodes",
          )}
          noResultsDescription={t(
            "awsThingsList.noResults.description",
            "No nodes match your search.",
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
    </div>
  );
}
