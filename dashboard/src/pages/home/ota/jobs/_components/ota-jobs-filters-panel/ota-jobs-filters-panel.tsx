/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { OtaJobStatusFilter } from "../ota-job-status-filter";
import { OtaJobTargetSelectionFilter } from "../ota-job-target-selection-filter";
import { OtaJobGroupFilter } from "../ota-job-group-filter";
import type { OtaJobsFiltersPanelProps } from "./ota-jobs-filters-panel.props";

export function OtaJobsFiltersPanel({
  filters,
  onStatusChange,
  onTargetSelectionChange,
  onGroupChange,
}: OtaJobsFiltersPanelProps) {
  return (
    <div className="flex items-center gap-5">
      <OtaJobStatusFilter
        value={filters.status ?? null}
        onChange={onStatusChange}
      />

      <OtaJobGroupFilter value={filters.groupName} onChange={onGroupChange} />

      <OtaJobTargetSelectionFilter
        value={filters.targetSelection ?? null}
        onChange={onTargetSelectionChange}
      />
    </div>
  );
}
