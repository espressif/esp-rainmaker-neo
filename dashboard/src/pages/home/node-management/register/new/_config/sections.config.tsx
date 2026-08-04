/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react";
import type { LucideIcon } from "lucide-react";
import type { TFunction } from "i18next";
import { FileText, Layers, Tag } from "lucide-react";
import { BasicDetailsSection } from "../_components/basic-details-section";
import { CapabilitiesSection } from "../_components/capabilities-section";
import { TagsSection } from "../_components/tags-section";

export interface RegisterNodesFormSection {
  id: string;
  Icon: LucideIcon;
  label: string;
  Content: ComponentType;
}

export function getRegisterNodesSections(
  t: TFunction,
): RegisterNodesFormSection[] {
  return [
    {
      id: "basic-details",
      Icon: FileText,
      label: t("new.sections.basicDetails", "Basic details"),
      Content: BasicDetailsSection,
    },
    {
      id: "capabilities",
      Icon: Layers,
      label: t("new.sections.capabilities", "Choose capabilities"),
      Content: CapabilitiesSection,
    },
    {
      id: "tags",
      Icon: Tag,
      label: t("new.sections.tags", "Add tags"),
      Content: TagsSection,
    },
  ];
}
