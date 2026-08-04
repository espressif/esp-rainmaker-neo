/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { JobStatus, TargetSelection } from "@aws-sdk/client-iot";
import type { OtaJobsFilters } from "../../ota-jobs.props";

export interface OtaJobsPageHeaderProps {
  filters: OtaJobsFilters;
  onStatusChange: (status: JobStatus | null) => void;
  onTargetSelectionChange: (targetSelection: TargetSelection | null) => void;
  onGroupChange: (groupName: string | undefined) => void;
  onClearAllFilters: () => void;
  onCreateClick: () => void;
}
