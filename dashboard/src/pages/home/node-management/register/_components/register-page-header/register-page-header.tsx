/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@espressif/dashboard-ui-components/components";
import { RegistrationJobStatusFilter } from "../registration-job-status-filter";
import type { RegisterPageHeaderProps } from "./register-page-header.props";

export function RegisterPageHeader({
  statusFilter,
  onStatusFilterChange,
  onRegisterClick,
}: RegisterPageHeaderProps) {
  const { t } = useTranslation("register");

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
        onClick={onRegisterClick}
        size="sm"
      >
        {t("registerNodesButton", "Register nodes")}
      </Button>
    </div>
  );
}
