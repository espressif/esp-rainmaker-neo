/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { StatusBadge } from "@/components/status-badge";
import { getNodeGroupStatusPresentation } from "@/config/node-group-status.config";
import type { NodeGroupStatusBadgeProps } from "./node-group-status-badge.props";

export function NodeGroupStatusBadge({ status }: NodeGroupStatusBadgeProps) {
  const { t } = useTranslation("node-groups");

  if (!status) {
    return null;
  }

  const { Icon, color, spinning, i18nKey, labelFallback } =
    getNodeGroupStatusPresentation(status);
  const label = i18nKey ? t(i18nKey, labelFallback) : status;

  return (
    <StatusBadge
      label={label}
      Icon={Icon}
      color={color}
      isLoading={spinning}
    />
  );
}
