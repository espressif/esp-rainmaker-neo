/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@espressif/dashboard-ui-components/components";
import { hasActiveOtaJobFilters } from "../../ota-jobs.props";
import { OtaJobsFiltersPanel } from "../ota-jobs-filters-panel";
import type { OtaJobsPageHeaderProps } from "./ota-jobs-page-header.props";

export function OtaJobsPageHeader({
  filters,
  onStatusChange,
  onTargetSelectionChange,
  onGroupChange,
  onClearAllFilters,
  onCreateClick,
}: OtaJobsPageHeaderProps) {
  const { t } = useTranslation(["ota-jobs", "common"]);
  const hasActiveFilters = hasActiveOtaJobFilters(filters);

  return (
    <div className="flex items-center justify-between gap-4 p-5 bg-accent/10 w-full">
      <OtaJobsFiltersPanel
        filters={filters}
        onStatusChange={onStatusChange}
        onTargetSelectionChange={onTargetSelectionChange}
        onGroupChange={onGroupChange}
      />

      <div className="flex items-center gap-2 shrink-0">
        {hasActiveFilters ? (
          <Button
            type="button"
            variant="link"
            color="error"
            onClick={onClearAllFilters}
            fullWidth={false}
            className="text-sm font-normal"
            size="sm"
          >
            {t("common:clearAllFilters", "Clear all filters")}
          </Button>
        ) : null}

        <Button
          variant="default"
          fullWidth={false}
          startIcon={<Plus className="h-4 w-4" aria-hidden />}
          onClick={onCreateClick}
          size="sm"
        >
          {t("createOtaJobButton", "Create OTA Job")}
        </Button>
      </div>
    </div>
  );
}
