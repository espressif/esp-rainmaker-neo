/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { ChevronDown } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";
import {
  Alert,
  AnimatedCard,
  Button,
  DataTable,
} from "@espressif/dashboard-ui-components/components";
import type { TagRow } from "./manage-thing-tags.utils";

interface ManageThingTagsContentProps {
  rows: TagRow[];
  visibleRows: TagRow[];
  columns: ColumnDef<TagRow>[];
  isLoading: boolean;
  isError: boolean;
  isRefetching: boolean;
  onRetry: () => void;
  emptyHeading: string;
  emptyDescription: string;
  remainingCount: number;
  onViewMore: () => void;
  pageSize: number;
}

export default function ManageThingTagsContent({
  rows,
  visibleRows,
  columns,
  isLoading,
  isError,
  isRefetching,
  onRetry,
  emptyHeading,
  emptyDescription,
  remainingCount,
  onViewMore,
  pageSize,
}: ManageThingTagsContentProps) {
  const { t } = useTranslation(["nodes", "common"]);

  if (isError) {
    return (
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
            loading={isRefetching}
            onClick={onRetry}
          >
            {t("common:actions.tryAgain", "Try again")}
          </Button>
        }
      >
        {t("tags.errorLoading", "Failed to load tags")}
      </AnimatedCard>
    );
  }

  if (!isLoading && rows.length === 0) {
    return (
      <Alert
        variant="soft"
        color="info"
        type="info"
        title={emptyHeading}
        description={emptyDescription}
        hideIcon
      />
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <DataTable
        columns={columns}
        data={visibleRows}
        isFetching={isLoading}
        pageSize={pageSize}
        hidePaginationControls
        showColumnVisibilitySelector={false}
        showBorder
        tableRowClassName="group"
      />
      {remainingCount > 0 ? (
        <div className="flex justify-center">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            color="gray"
            fullWidth={false}
            endIcon={<ChevronDown className="h-4 w-4" />}
            onClick={onViewMore}
          >
            {t("tags.viewMore", "View {{count}} more", {
              count: remainingCount,
            })}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
