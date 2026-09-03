/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { StatusBadge } from "@espressif/dashboard-ui-components/components";
import { getSesAccessStatusPresentation } from "@/config/ses-access-status.config";
import type { SesAccessStatusBadgeProps } from "./ses-access-status-badge.props";

export function SesAccessStatusBadge({ status }: SesAccessStatusBadgeProps) {
  const { t } = useTranslation("post-deployment");

  if (!status) {
    return null;
  }

  const { Icon, color, i18nKey, labelFallback } =
    getSesAccessStatusPresentation(status);
  const label = i18nKey ? t(i18nKey, labelFallback) : status;

  return <StatusBadge label={label} Icon={Icon} color={color} variant="gradient" />;
}
