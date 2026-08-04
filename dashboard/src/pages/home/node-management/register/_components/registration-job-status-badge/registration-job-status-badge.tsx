/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { StatusBadge } from "@/components/status-badge";
import { getRegistrationJobStatusPresentation } from "@/config/registration-job-status.config";
import type { RegistrationJobStatusBadgeProps } from "./registration-job-status-badge.props";

export function RegistrationJobStatusBadge({
  status,
}: RegistrationJobStatusBadgeProps) {
  const { t } = useTranslation("nodes");
  const { Icon, color, spinning, i18nKey, labelFallback } =
    getRegistrationJobStatusPresentation(status);
  const label = i18nKey ? t(i18nKey, labelFallback) : (status ?? "");

  if (!label) {
    return null;
  }

  return (
    <StatusBadge
      label={label}
      Icon={Icon}
      color={color}
      variant="gradient"
      isLoading={spinning}
    />
  );
}
