/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Badge } from "@espressif/dashboard-ui-components/components";
import {
  getTargetSelectionPresentation,
  isTargetSelection,
} from "@/config/target-selection.config";

interface TargetSelectionBadgeProps {
  selection?: string;
}

/** Solid badge with the target-selection icon (Continuous / Snapshot). */
export default function TargetSelectionBadge({
  selection,
}: TargetSelectionBadgeProps) {
  const { t } = useTranslation("ota-jobs");
  if (!isTargetSelection(selection)) {
    return null;
  }
  const { Icon, color, i18nKey, labelFallback } =
    getTargetSelectionPresentation(selection);
  return (
    <Badge color={color} variant="solid" className="gap-1.5 font-normal">
      <Icon className="h-3.5 w-3.5 shrink-0" aria-hidden />
      {t(i18nKey, labelFallback)}
    </Badge>
  );
}
