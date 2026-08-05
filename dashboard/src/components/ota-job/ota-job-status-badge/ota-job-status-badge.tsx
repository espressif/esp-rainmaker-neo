/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { StatusBadge } from "@/components/status-badge";
import { getOtaJobStatusPresentation } from "@/config/ota-job-status.config";
import type { OtaJobStatusBadgeProps } from "./ota-job-status-badge.props";

export function OtaJobStatusBadge({ status }: OtaJobStatusBadgeProps) {
  const { t } = useTranslation("common");
  const { Icon, color, spinning, i18nKey } = getOtaJobStatusPresentation(status);
  // Statuses AWS returns that carry no `i18nKey` fall back to the raw status.
  const label = i18nKey ? t(i18nKey, status ?? "") : (status ?? "");

  if (!label) {
    return null;
  }

  return (
    <StatusBadge label={label} Icon={Icon} color={color} isLoading={spinning} />
  );
}
