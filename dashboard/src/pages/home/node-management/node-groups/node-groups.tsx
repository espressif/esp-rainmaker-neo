/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import {
  DataTable,
  FullSizeError,
  PageContainer,
  PageContainerSkeleton,
} from "@espressif/dashboard-ui-components/components";
import { useTranslation } from "react-i18next";
import { getNodeGroupsColumns } from "./_columns";
import { NodeGroupsPageHeader } from "./_components/node-groups-page-header";
import { useNodeGroups } from "./use-node-groups";

function NodeGroups() {
  const { t } = useTranslation(["node-groups", "common"]);
  const navigate = useNavigate();
  const location = useLocation();

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
    handleSearch,
  } = useNodeGroups();

  const columns = useMemo(() => getNodeGroupsColumns(t), [t]);

  // This route is the parent of its detail (`/$groupName`) and create (`/new`)
  // subroutes. Render the child via <Outlet /> whenever we are on one of them;
  // only the bare list path renders the table below.
  const listPath = "/home/node-management/node-groups";
  const isListRoute =
    location.pathname === listPath || location.pathname === `${listPath}/`;

  if (!isListRoute) {
    return <Outlet />;
  }

  if (isLoading) {
    return (
      <PageContainerSkeleton maxWidth="xl" showHeader showActions={false} />
    );
  }

  if (error) {
    return (
      <FullSizeError title={t("error.title", "Failed to load node groups")}>
        {error instanceof Error ? error.message : t("error.description", "An unexpected error occurred while loading node groups. Please try again later.")}
      </FullSizeError>
    );
  }

  return (
    <PageContainer
      noGutters
      className="p-0"
      elevateHeading
      heading={
        <NodeGroupsPageHeader
          onSearch={handleSearch}
          onCreateClick={() =>
            void navigate({ to: "/home/node-management/node-groups/new" })
          }
        />
      }
    >
      <div className="px-5 pb-5">
        <DataTable
          columns={columns}
          data={rows}
          onRowClick={(row) =>
            void navigate({
              to: "/home/node-management/node-groups/$groupName",
              params: { groupName: row.groupName },
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
          noResultsHeading={t("noResults", "No results.")}
          noResultsDescription={t("noResultsDescription", "No node groups found. Try adjusting your search.")}
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

export default NodeGroups;
