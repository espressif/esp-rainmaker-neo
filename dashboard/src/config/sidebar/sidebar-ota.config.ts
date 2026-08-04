/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type { SidebarGroupConfig } from "@espressif/dashboard-ui-components/components";
import { getCreateEndAction } from "@/config/create-resources.config";
import { sidebarIcons } from "./sidebar-icons.config";

export function getOtaSidebarGroup(t: TFunction): SidebarGroupConfig {
  const group = sidebarIcons.ota;
  return {
    id: "ota",
    label: t("sidebar.ota", "OTA"),
    icon: group.icon,
    defaultExpanded: true,
    items: [
      {
        id: "images",
        label: t("sidebar.otaImages", "Images"),
        icon: group.items.images,
        path: "/home/ota/images",
        endAction: getCreateEndAction(t, "ota-image"),
      },
      {
        id: "jobs",
        label: t("sidebar.otaJobs", "Jobs"),
        icon: group.items.jobs,
        path: "/home/ota/jobs",
        endAction: getCreateEndAction(t, "ota-job"),
      },
    ],
  };
}
