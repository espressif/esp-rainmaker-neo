/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Badge, Spinner } from "@espressif/dashboard-ui-components/components";
import { getNodeGroupStatusPresentation } from "@/config/node-group-status.config";
import type { NodeGroupStatusBadgeProps } from "./node-group-status-badge.props";

const ICON_SIZE_PX = 14;

export function NodeGroupStatusBadge({ status }: NodeGroupStatusBadgeProps) {
  const { t } = useTranslation("node-groups");

  if (!status) {
    return null;
  }

  const { Icon, color, spinning, i18nKey, labelFallback } =
    getNodeGroupStatusPresentation(status);
  const label = i18nKey ? t(i18nKey, labelFallback) : status;

  return (
    <Badge color={color} variant="soft" className="font-normal gap-1.5">
      {spinning ? (
        <Spinner size={ICON_SIZE_PX} className="shrink-0" aria-hidden />
      ) : (
        <Icon className="h-3.5 w-3.5 shrink-0" aria-hidden />
      )}
      {label}
    </Badge>
  );
}
