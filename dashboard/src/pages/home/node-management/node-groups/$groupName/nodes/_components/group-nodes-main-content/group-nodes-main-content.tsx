/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  AnimatedCard,
  Button,
  DataTable,
} from "@espressif/dashboard-ui-components/components";
import { useNodeGroupTypeQuery } from "@/api/node-groups";
import { DynamicMembershipNotice } from "../dynamic-membership-notice";
import { GroupNodesCardShell } from "../group-nodes-card-shell";
import { getGroupNodesColumns } from "../../_columns";
import { useGroupNodes } from "../../use-group-nodes";
import type { GroupNodesMainContentProps } from "./group-nodes-main-content.props";

export default function GroupNodesMainContent({
  groupName,
}: GroupNodesMainContentProps) {
  const { t } = useTranslation(["node-groups", "common"]);
  const navigate = useNavigate();
  const { isDynamic, queryString } = useNodeGroupTypeQuery(groupName);

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
  } = useGroupNodes(groupName);

  const columns = useMemo(
    () => getGroupNodesColumns(t, groupName, !isDynamic),
    [t, groupName, isDynamic],
  );

  if (error) {
    return (
      <GroupNodesCardShell isDynamic={isDynamic}>
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
          {t("details.nodes.error.title", "Failed to load nodes")}
        </AnimatedCard>
      </GroupNodesCardShell>
    );
  }

  return (
    <GroupNodesCardShell isDynamic={isDynamic}>
      {isDynamic && <DynamicMembershipNotice queryString={queryString} />}
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
            to: "/home/node-management/nodes/$thingName",
            params: { thingName: row.awsThingName },
          })
        }
        tableRowClassName="group"
        showBorder
        showColumnVisibilitySelector={false}
        noResultsHeading={t(
          "details.nodes.noResults.title",
          "No nodes",
        )}
        noResultsDescription={t(
          "details.nodes.noResults.description",
          "There are no nodes in this group yet.",
        )}
        pageLabel={t("common:dataTable.page", "Page {{current}}", {
          current: pagination.pageIndex + 1,
        })}
        previousLabel={t("common:dataTable.previous", "Previous")}
        nextLabel={t("common:dataTable.next", "Next")}
        columnVisibilityLabel={t(
          "common:dataTable.columnVisibility.label",
          "Columns",
        )}
        pageSizeOptionLabel={(size) =>
          `${size} ${t("common:dataTable.rowsPerPage", "rows per page")}`
        }
      />
    </GroupNodesCardShell>
  );
}
