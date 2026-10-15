/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react";
import type { LucideIcon } from "lucide-react";
import type { TFunction } from "i18next";
import { FileText, Tag } from "lucide-react";
import { BasicDetailsSection } from "../_components/basic-details-section";
import { TagsSection } from "../_components/tags-section";

export interface UpdateNodesFormSection {
  id: string;
  Icon: LucideIcon;
  label: string;
  Content: ComponentType;
}

export function getUpdateNodesSections(
  t: TFunction,
): UpdateNodesFormSection[] {
  return [
    {
      id: "basic-details",
      Icon: FileText,
      label: t("new.sections.basicDetails", "Basic details"),
      Content: BasicDetailsSection,
    },
    {
      id: "tags",
      Icon: Tag,
      label: t("new.sections.tags", "Add tags"),
      Content: TagsSection,
    },
  ];
}
