/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@espressif/dashboard-ui-components/components";
import { RegistrationJobStatusFilter } from "../../../register/_components/registration-job-status-filter";
import type { UpdatePageHeaderProps } from "./update-page-header.props";

export function UpdatePageHeader({
  statusFilter,
  onStatusFilterChange,
  onUpdateClick,
}: UpdatePageHeaderProps) {
  const { t } = useTranslation("update");

  return (
    <div className="flex items-center justify-between gap-4 p-5 bg-accent/10 w-full">
      <RegistrationJobStatusFilter
        value={statusFilter}
        onChange={onStatusFilterChange}
      />
      <Button
        variant="default"
        fullWidth={false}
        startIcon={<Plus className="h-4 w-4" aria-hidden />}
        onClick={onUpdateClick}
        size="sm"
      >
        {t("updateNodesButton", "Update nodes")}
      </Button>
    </div>
  );
}
