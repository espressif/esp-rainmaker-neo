/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { StatusBadge } from "@espressif/dashboard-ui-components/components";
import {
  getThingDisplayStatus,
  getThingStatusPresentation,
} from "@/config/node-status.config";
import type { ThingStatusBadgeProps } from "./thing-status-badge.props";

export function ThingStatusBadge({ online }: ThingStatusBadgeProps) {
  const { t } = useTranslation("common");
  const status = getThingDisplayStatus(online);

  if (!status) {
    return null;
  }

  const { Icon, color, i18nKey, labelFallback } =
    getThingStatusPresentation(status);

  return (
    <StatusBadge
      label={t(i18nKey, labelFallback)}
      Icon={Icon}
      color={color}
      variant="gradient"
    />
  );
}
