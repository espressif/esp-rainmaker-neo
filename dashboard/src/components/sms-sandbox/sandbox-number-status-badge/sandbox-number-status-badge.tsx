/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { StatusBadge } from "@espressif/dashboard-ui-components/components";
import { getSandboxNumberStatusPresentation } from "@/config/sandbox-number-status.config";
import type { SandboxNumberStatusBadgeProps } from "./sandbox-number-status-badge.props";

export function SandboxNumberStatusBadge({
  status,
}: SandboxNumberStatusBadgeProps) {
  const { t } = useTranslation("post-deployment");

  if (!status) {
    return null;
  }

  const { Icon, color, i18nKey, labelFallback } =
    getSandboxNumberStatusPresentation(status);
  const label = i18nKey ? t(i18nKey, labelFallback) : status;

  return (
    <StatusBadge
      label={label}
      Icon={Icon}
      color={color}
      variant="gradient"
      size="sm"
    />
  );
}
