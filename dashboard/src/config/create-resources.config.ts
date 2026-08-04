/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react";
import type { TFunction } from "i18next";
import type { SidebarEndActionConfig } from "@espressif/dashboard-ui-components/components";
import { CloudUpload, Cpu, FileStack, Group, Plus } from "lucide-react";

type ResourceIcon = ComponentType<{ className?: string }>;

export type CreatableResourceId =
  | "node-group"
  | "register"
  | "ota-image"
  | "ota-job";

/** Ordered domain groups the quick-create menu renders, separated visually. */
export const CREATE_RESOURCE_GROUPS = ["node-management", "ota"] as const;

export type CreateResourceGroupId = (typeof CREATE_RESOURCE_GROUPS)[number];

export type CreatableResource = {
  /** Stable identifier for the resource. */
  id: CreatableResourceId;
  /** Domain group the resource belongs to in the quick-create menu. */
  group: CreateResourceGroupId;
  /** i18n key under the `common` namespace. */
  labelKey: string;
  /** English fallback used until the key is translated. */
  fallback: string;
  /** Resource identity icon shown in the quick-create menu. */
  icon: ResourceIcon;
  /** Route to the resource's create page. */
  path: string;
};

/**
 * Single source of truth for every "create new resource" entry.
 *
 * Drives BOTH the header quick-create menu ([`create-menu`](../components/create-menu/create-menu.tsx))
 * and the sidebar `+` end-actions (via {@link getCreateEndAction}). Add or remove a
 * creatable resource here and both surfaces update automatically — do not duplicate
 * create routes elsewhere.
 */
export const CREATABLE_RESOURCES = [
  {
    id: "node-group",
    group: "node-management",
    labelKey: "common:createMenu.nodeGroup",
    fallback: "Node group",
    icon: Group,
    path: "/home/node-management/node-groups/new",
  },
  {
    id: "register",
    group: "node-management",
    labelKey: "common:createMenu.registerNodes",
    fallback: "Register nodes",
    icon: FileStack,
    path: "/home/node-management/register/new",
  },
  {
    id: "ota-image",
    group: "ota",
    labelKey: "common:createMenu.otaImage",
    fallback: "Upload OTA Image",
    icon: Cpu,
    path: "/home/ota/images/new",
  },
  {
    id: "ota-job",
    group: "ota",
    labelKey: "common:createMenu.otaJob",
    fallback: "OTA Job",
    icon: CloudUpload,
    path: "/home/ota/jobs/new",
  },
] as const satisfies readonly CreatableResource[];

const CREATABLE_RESOURCES_BY_ID = Object.fromEntries(
  CREATABLE_RESOURCES.map((resource) => [resource.id, resource]),
) as Record<CreatableResourceId, (typeof CREATABLE_RESOURCES)[number]>;

/**
 * Projects a creatable resource into a sidebar `endAction` (the `+` button on a
 * sidebar row). The glyph is always `Plus`; the tooltip reuses the resource label.
 */
export function getCreateEndAction(
  t: TFunction,
  id: CreatableResourceId,
): SidebarEndActionConfig {
  const resource = CREATABLE_RESOURCES_BY_ID[id];
  return {
    icon: Plus,
    path: resource.path,
    tooltip: t(resource.labelKey, resource.fallback),
  };
}
