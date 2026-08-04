/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import {
  CHART_SERIES_COLORS,
  ProgressBar,
  type ProgressBarSegment,
} from "@espressif/dashboard-ui-components/components";
import type { RegistrationJobProgressCellProps } from "./registration-job-progress-cell.props";

export function RegistrationJobProgressCell({
  successCount,
  failedCount,
  totalNodes,
}: RegistrationJobProgressCellProps) {
  const label = `${successCount}/${totalNodes}`;

  const segments = useMemo<ProgressBarSegment[]>(
    () => [
      { value: successCount, color: CHART_SERIES_COLORS.color20 },
      { value: failedCount, color: CHART_SERIES_COLORS.color18 },
    ],
    [successCount, failedCount],
  );

  return (
    <div className="min-w-[10rem] pl-1">
      <ProgressBar
        label={label}
        segments={segments}
        className="w-full max-w-[160px]"
      />
    </div>
  );
}
