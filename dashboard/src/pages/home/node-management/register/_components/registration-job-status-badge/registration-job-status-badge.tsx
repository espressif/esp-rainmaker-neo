/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Badge } from "@espressif/dashboard-ui-components/components";
import { getRegistrationJobStatusPresentation } from "@/config/registration-job-status.config";
import type { RegistrationJobStatusBadgeProps } from "./registration-job-status-badge.props";

export function RegistrationJobStatusBadge({
  status,
}: RegistrationJobStatusBadgeProps) {
  const { t } = useTranslation("nodes");
  const { Icon, color, i18nKey, labelFallback } =
    getRegistrationJobStatusPresentation(status);
  const label = i18nKey ? t(i18nKey, labelFallback) : (status ?? "");

  if (!label) {
    return null;
  }

  return (
    <Badge color={color} variant="soft" className="font-normal gap-1.5">
      <Icon className="h-3.5 w-3.5 shrink-0" aria-hidden />
      {label}
    </Badge>
  );
}
