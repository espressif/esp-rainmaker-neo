/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TargetSelection } from "@aws-sdk/client-iot";
import type { LucideIcon } from "lucide-react";
import { ScanEye, Repeat } from "lucide-react";
import type { Color } from "@espressif/dashboard-ui-components";

export interface TargetSelectionPresentation {
  Icon: LucideIcon;
  color: Color;
  /** i18n key under the `ota-jobs` namespace. */
  i18nKey: string;
  labelFallback: string;
}

export const TARGET_SELECTION_PRESENTATION: Record<
  TargetSelection,
  TargetSelectionPresentation
> = {
  CONTINUOUS: {
    Icon: Repeat,
    color: "success",
    i18nKey: "ota-jobs:targetSelection.CONTINUOUS",
    labelFallback: "Continuous",
  },
  SNAPSHOT: {
    Icon: ScanEye,
    color: "secondary",
    i18nKey: "ota-jobs:targetSelection.SNAPSHOT",
    labelFallback: "Snapshot",
  },
};

/** Ordered iteration for the target selection filter dropdown. */
export const TARGET_SELECTION_IDS: readonly TargetSelection[] = [
  "CONTINUOUS",
  "SNAPSHOT",
];

const UNKNOWN_TARGET_SELECTION_PRESENTATION: TargetSelectionPresentation = {
  Icon: Repeat,
  color: "gray",
  i18nKey: "",
  labelFallback: "",
};

export function isTargetSelection(
  value: string | null | undefined,
): value is TargetSelection {
  return !!value && value in TARGET_SELECTION_PRESENTATION;
}

export function getTargetSelectionPresentation(
  value: string | null | undefined,
): TargetSelectionPresentation {
  if (isTargetSelection(value)) {
    return TARGET_SELECTION_PRESENTATION[value];
  }
  return UNKNOWN_TARGET_SELECTION_PRESENTATION;
}
