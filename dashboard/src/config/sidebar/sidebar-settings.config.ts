/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type { SidebarGroupConfig } from "@espressif/dashboard-ui-components/components";
import { sidebarIcons } from "./sidebar-icons.config";

export function getSettingsSidebarGroup(t: TFunction): SidebarGroupConfig {
  const group = sidebarIcons.settings;
  return {
    id: "settings",
    label: t("sidebar.settings", "Settings"),
    icon: group.icon,
    defaultExpanded: true,
    items: [
      {
        id: "voice-assistants",
        label: t("sidebar.voiceAssistants", "Voice assistants"),
        icon: group.items["voice-assistants"],
        path: "/home/settings/voice-assistants",
      },
      {
        id: "push-notifications",
        label: t("sidebar.pushNotifications", "Push notifications"),
        icon: group.items["push-notifications"],
        path: "/home/settings/push-notifications",
      },
      {
        id: "post-deployment",
        label: t("sidebar.postDeployment", "Post-Deployment"),
        icon: group.items["post-deployment"],
        path: "/home/settings/post-deployment",
      },
    ],
  };
}
