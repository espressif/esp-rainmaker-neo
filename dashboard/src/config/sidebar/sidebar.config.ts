/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type { SidebarNavEntry } from "@espressif/dashboard-ui-components/layouts";
import { getNodeManagementSidebarGroup } from "./sidebar-node-management.config";
import { getOtaSidebarGroup } from "./sidebar-ota.config";
import { getSettingsSidebarGroup } from "./sidebar-settings.config";

/**
 * Sidebar configuration — call with `t` from `useTranslation("common")` for labels.
 */
export function getSidebarConfig(t: TFunction): SidebarNavEntry[] {
  return [
    getNodeManagementSidebarGroup(t),
    getOtaSidebarGroup(t),
    getSettingsSidebarGroup(t),
  ];
}
