/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LucideIcon } from "lucide-react";
import { CheckCircle2, XCircle } from "lucide-react";
import type { Color } from "@espressif/dashboard-ui-components";
import type { AnimatedCardType } from "@espressif/dashboard-ui-components/components";

const PRESET_COLOR_TEXT_CLASS: Record<string, string> = {
  primary: "text-primary",
  secondary: "text-secondary",
  error: "text-error",
  info: "text-info",
  success: "text-success",
  warning: "text-warning",
  disabled: "text-disabled",
  gray: "text-gray",
};

export function getPresetColorTextClass(color: Color): string {
  return PRESET_COLOR_TEXT_CLASS[color] ?? "";
}

export type ThingDisplayStatus = "online" | "offline";

export interface ThingStatusPresentation {
  Icon: LucideIcon;
  color: Color;
  /**
   * Fully qualified: consumers bind different namespaces (the badge lives in
   * `src/components`, the filter in the nodes page), so a bare key would resolve
   * for some of them and render as the raw key for the rest.
   */
  i18nKey: string;
  /** English fallback, so a missing translation never surfaces the key itself. */
  labelFallback: string;
}

export const THING_STATUS_PRESENTATION: Record<ThingDisplayStatus, ThingStatusPresentation> = {
  online: {
    Icon: CheckCircle2,
    color: "success",
    i18nKey: "common:nodeStatus.online",
    labelFallback: "Online",
  },
  offline: {
    Icon: XCircle,
    color: "error",
    i18nKey: "common:nodeStatus.offline",
    labelFallback: "Offline",
  },
};

/** Ordered iteration for the status filter dropdown. */
export const THING_STATUS_IDS: readonly ThingDisplayStatus[] = ["online", "offline"];

const UNKNOWN_STATUS_PRESENTATION: ThingStatusPresentation = {
  Icon: CheckCircle2,
  color: "gray",
  i18nKey: "",
  labelFallback: "",
};

export function getThingStatusPresentation(
  status: ThingDisplayStatus | null | undefined,
): ThingStatusPresentation {
  if (status && status in THING_STATUS_PRESENTATION) {
    return THING_STATUS_PRESENTATION[status];
  }
  return UNKNOWN_STATUS_PRESENTATION;
}

export function getThingDisplayStatus(
  online: boolean | null,
): ThingDisplayStatus | null {
  if (online === true) {
    return "online";
  }
  if (online === false) {
    return "offline";
  }
  return null;
}

const THING_STATUS_ANIMATED_CARD_TYPE: Record<ThingDisplayStatus, AnimatedCardType> = {
  online: "active",
  offline: "crossThrob",
};

export function getAnimatedCardTypeForThingStatus(
  status: ThingDisplayStatus | null,
): AnimatedCardType {
  if (!status) {
    return "inactive";
  }
  return THING_STATUS_ANIMATED_CARD_TYPE[status];
}
