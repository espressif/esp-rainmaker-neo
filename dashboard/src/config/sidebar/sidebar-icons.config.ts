/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react";
import {
  BellRing,
  Bot,
  CloudUpload,
  Cpu,
  FileStack,
  Group,
  Package,
  Server,
  Settings,
  ShieldCheck,
  Wand2,
  Workflow,
} from "lucide-react";

export type SidebarIcon = ComponentType<{ className?: string }>;

/**
 * Canonical icon map for every sidebar group and item. Item keys match
 * `SidebarItemConfig.id` exactly.
 */
export const sidebarIcons = {
  "node-management": {
    icon: Workflow,
    items: {
      nodes: Server,
      "node-groups": Group,
      register: FileStack,
      generate: Wand2,
    },
  },
  ota: {
    icon: Package,
    items: {
      images: Cpu,
      jobs: CloudUpload,
    },
  },
  settings: {
    icon: Settings,
    items: {
      "voice-assistants": Bot,
      "push-notifications": BellRing,
      "post-deployment": ShieldCheck,
    },
  },
} as const satisfies Record<
  string,
  { icon: SidebarIcon; items: Record<string, SidebarIcon> }
>;
