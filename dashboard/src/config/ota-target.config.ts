/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  sidebarIcons,
  type SidebarIcon,
} from "@/config/sidebar/sidebar-icons.config";

/** AWS IoT job targets are either individual things or thing groups. */
export type OtaTargetType = "thing" | "thinggroup";

export interface OtaTarget {
  /** The raw AWS ARN. */
  arn: string;
  /** Display name extracted from the ARN (the segment after the last `/`). */
  name: string;
  type: OtaTargetType;
}

export interface OtaTargetTypePresentation {
  Icon: SidebarIcon;
  /** i18n key under the `ota-jobs` namespace. */
  i18nKey: string;
  labelFallback: string;
}

/**
 * A thing-group ARN looks like `arn:aws:iot:<region>:<acct>:thinggroup/<name>`,
 * a thing ARN like `arn:aws:iot:<region>:<acct>:thing/<name>`. We match the
 * `:thinggroup/` resource prefix so the more specific type wins.
 */
export function parseOtaTarget(arn: string): OtaTarget {
  const name = arn.split("/").pop() ?? arn;
  const type: OtaTargetType = arn.includes(":thinggroup/")
    ? "thinggroup"
    : "thing";
  return { arn, name, type };
}

/**
 * Icons mirror the sidebar navigation so a target reads the same as the section
 * it links to: things → "Nodes" (Server), thing groups → "Node groups" (Group).
 */
export const OTA_TARGET_TYPE_PRESENTATION: Record<
  OtaTargetType,
  OtaTargetTypePresentation
> = {
  thing: {
    Icon: sidebarIcons["node-management"].items.nodes,
    i18nKey: "ota-jobs:details.overview.target.type.thing",
    labelFallback: "Node",
  },
  thinggroup: {
    Icon: sidebarIcons["node-management"].items["node-groups"],
    i18nKey: "ota-jobs:details.overview.target.type.thinggroup",
    labelFallback: "Node group",
  },
};
