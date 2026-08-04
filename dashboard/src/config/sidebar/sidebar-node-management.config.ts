/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type { SidebarGroupConfig } from "@espressif/dashboard-ui-components/components";
import { getCreateEndAction } from "@/config/create-resources.config";
import { sidebarIcons } from "./sidebar-icons.config";

export function getNodeManagementSidebarGroup(
  t: TFunction,
): SidebarGroupConfig {
  const group = sidebarIcons["node-management"];
  return {
    id: "node-management",
    label: t("sidebar.nodeManagement", "Node management"),
    icon: group.icon,
    defaultExpanded: true,
    items: [
      {
        id: "nodes",
        label: t("sidebar.nodes", "Nodes"),
        icon: group.items.nodes,
        path: "/home/node-management/nodes",
      },
      {
        id: "node-groups",
        label: t("sidebar.nodeGroups", "Node groups"),
        icon: group.items["node-groups"],
        path: "/home/node-management/node-groups",
        endAction: getCreateEndAction(t, "node-group"),
      },
      {
        id: "generate",
        label: t("sidebar.generateNodes", "Generate nodes"),
        icon: group.items.generate,
        path: "/home/node-management/generate",
      },
      {
        id: "register",
        label: t("sidebar.registerNodes", "Register nodes"),
        icon: group.items.register,
        path: "/home/node-management/register",
        endAction: getCreateEndAction(t, "register"),
      },
    ],
  };
}
