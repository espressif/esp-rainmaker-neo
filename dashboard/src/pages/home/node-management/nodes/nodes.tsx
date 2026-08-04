/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useNavigate, Outlet } from "@tanstack/react-router";
import { DataTable } from "@espressif/dashboard-ui-components/components";
import { FullSizeError } from "@espressif/dashboard-ui-components/components";
import {
  PageContainer,
  PageContainerSkeleton,
} from "@espressif/dashboard-ui-components/components";
import { useTranslation } from "react-i18next";
import {
  advancedSearchFieldsData,
  type IndexField,
} from "@/aws/components/advanced-indices-search";
import { getNodesColumns } from "./_columns";
import { NodesPageHeader } from "./_components/nodes-page-header";
import { NodesTableActions } from "./_components/nodes-table-actions/nodes-table-actions";
import { useNodes } from "./use-nodes";
import { useRouteParams } from "@/lib/navigation/use-route-params";

const advancedSearchFields: IndexField[] = advancedSearchFieldsData.map(
  ({ icon: Icon, ...field }) => ({
    ...field,
    icon: Icon ? <Icon /> : undefined,
  }),
);

function IoTThings() {
  const { t } = useTranslation(["nodes", "common"]);
  const navigate = useNavigate();
  const params = useRouteParams<{ thingName?: string }>();
  const listEnabled = !params.thingName;

  const {
    filters,
    searchBoxKey,
    pagination,
    things,
    isLoading,
    error,
    isFetching,
    hasNextPage,
    hasPrevPage,
    handleSearch,
    handleSearchClear,
    handleStatusChange,
    handleTypeModelChange,
    handleFirmwareVersionChange,
    handleAdvancedSearch,
    handleClearAllFilters,
    handlePageSizeChange,
    handleNextPage,
    handlePrevPage,
  } = useNodes(listEnabled);

  const columns = useMemo(() => getNodesColumns(t), [t]);

  if (params.thingName) {
    return <Outlet />;
  }

  if (isLoading) {
    return (
      <PageContainerSkeleton maxWidth="xl" showHeader showActions={false} />
    );
  }

  if (error) {
    return (
      <FullSizeError title={t("error.title", "Failed to load nodes")}>
        {error instanceof Error ? error.message : t("error.description", "An unexpected error occurred while loading nodes. Please try again later.")}
      </FullSizeError>
    );
  }

  return (
    <PageContainer
      noGutters
      className="p-0"
      elevateHeading
      heading={
        <NodesPageHeader
          filters={filters}
          searchBoxKey={searchBoxKey}
          advancedSearchFields={advancedSearchFields}
          onSearch={handleSearch}
          onSearchClear={handleSearchClear}
          onStatusFilterChange={handleStatusChange}
          onTypeModelFilterChange={handleTypeModelChange}
          onFirmwareVersionChange={handleFirmwareVersionChange}
          onAdvancedSearch={handleAdvancedSearch}
          onClearAllFilters={handleClearAllFilters}
        />
      }
    >
      <div className="px-5 pb-5">
        <DataTable
          columns={columns}
          data={things}
          onRowClick={(row) =>
            void navigate({
              to: "/home/node-management/nodes/$thingName",
              params: { thingName: row.awsThingName },
            })
          }
          tableActionsContent={<NodesTableActions />}
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
          noResultsHeading={t("noSearchResults.title", "No nodes found")}
          noResultsDescription={t("noSearchResults.description", "Try adjusting your search or filter to find the node you're looking for.")}
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

export default IoTThings;
