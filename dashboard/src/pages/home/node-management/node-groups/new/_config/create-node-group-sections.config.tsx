/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react";
import type { LucideIcon } from "lucide-react";
import type { TFunction } from "i18next";
import { FileText, FolderTree, Zap } from "lucide-react";
import { BasicDetailsSection } from "../_components/basic-details-section";
import { SubgroupSection } from "../_components/subgroup-section";
import { DynamicGroupSection } from "../_components/dynamic-group-section";
import {
  SECTION_BASIC_DETAILS,
  SECTION_DYNAMIC,
  SECTION_SUBGROUP,
} from "../_constants/create-node-group-form.constants";

export interface CreateNodeGroupFormSection {
  id: string;
  Icon: LucideIcon;
  label: string;
  secondaryText: string;
  Content: ComponentType;
}

export function getCreateNodeGroupSections(
  t: TFunction,
): CreateNodeGroupFormSection[] {
  return [
    {
      id: SECTION_BASIC_DETAILS,
      Icon: FileText,
      label: t("new.sections.basicDetails.label", "Basic details"),
      secondaryText: t(
        "new.sections.basicDetails.description",
        "Name this group and describe what it is for.",
      ),
      Content: BasicDetailsSection,
    },
    {
      id: SECTION_SUBGROUP,
      Icon: FolderTree,
      label: t("new.sections.subgroup.label", "Create as sub-group"),
      secondaryText: t(
        "new.sections.subgroup.description",
        "Optionally nest this group under a parent group.",
      ),
      Content: SubgroupSection,
    },
    {
      id: SECTION_DYNAMIC,
      Icon: Zap,
      label: t("new.sections.dynamic.label", "Create as dynamic group"),
      secondaryText: t(
        "new.sections.dynamic.description",
        "Optionally populate this group automatically from matching rules.",
      ),
      Content: DynamicGroupSection,
    },
  ];
}
