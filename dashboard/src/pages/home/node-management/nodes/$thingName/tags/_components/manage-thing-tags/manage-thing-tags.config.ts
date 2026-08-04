/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Cpu, ShieldCheck, User } from "lucide-react";
import type { LucideIcon } from "lucide-react";

export type ManageThingTagsType = "admin" | "user" | "device";

export interface ManageThingTagsSectionConfig {
  Icon: LucideIcon;
  titleKey: string;
  titleFallback: string;
  descriptionKey: string;
  descriptionFallback: string;
  emptyHeadingKey: string;
  emptyHeadingFallback: string;
  emptyDescriptionKey: string;
  emptyDescriptionFallback: string;
}

export const MANAGE_THING_TAGS_CONFIG: Record<
  ManageThingTagsType,
  ManageThingTagsSectionConfig
> = {
  admin: {
    Icon: ShieldCheck,
    titleKey: "tags.sections.admin.title",
    titleFallback: "Admin tags",
    descriptionKey: "tags.sections.admin.description",
    descriptionFallback: "Tags managed by admins.",
    emptyHeadingKey: "tags.sections.admin.emptyHeading",
    emptyHeadingFallback: "No admin tags yet",
    emptyDescriptionKey: "tags.sections.admin.emptyDescription",
    emptyDescriptionFallback: "Add an admin tag to categorize this node.",
  },
  user: {
    Icon: User,
    titleKey: "tags.sections.user.title",
    titleFallback: "User tags",
    descriptionKey: "tags.sections.user.description",
    descriptionFallback: "Tags added by end users.",
    emptyHeadingKey: "tags.sections.user.emptyHeading",
    emptyHeadingFallback: "No user tags yet",
    emptyDescriptionKey: "tags.sections.user.emptyDescription",
    emptyDescriptionFallback: "End users have not added tags for this node.",
  },
  device: {
    Icon: Cpu,
    titleKey: "tags.sections.device.title",
    titleFallback: "Device tags",
    descriptionKey: "tags.sections.device.description",
    descriptionFallback: "Tags reported by the device (read-only).",
    emptyHeadingKey: "tags.sections.device.emptyHeading",
    emptyHeadingFallback: "No device tags reported",
    emptyDescriptionKey: "tags.sections.device.emptyDescription",
    emptyDescriptionFallback: "This device has not reported any tags.",
  },
};
