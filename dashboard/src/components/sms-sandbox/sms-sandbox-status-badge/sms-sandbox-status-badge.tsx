/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Badge } from "@espressif/dashboard-ui-components/components";
import { getSmsSandboxStatusPresentation } from "@/config/sms-sandbox-status.config";
import type { SmsSandboxStatusBadgeProps } from "./sms-sandbox-status-badge.props";

export function SmsSandboxStatusBadge({ status }: SmsSandboxStatusBadgeProps) {
  const { t } = useTranslation("post-deployment");

  if (!status) {
    return null;
  }

  const { Icon, color, i18nKey, labelFallback } =
    getSmsSandboxStatusPresentation(status);
  const label = i18nKey ? t(i18nKey, labelFallback) : status;

  return (
    <Badge color={color} variant="soft" className="font-normal gap-1.5">
      <Icon className="h-3.5 w-3.5 shrink-0" aria-hidden />
      {label}
    </Badge>
  );
}
