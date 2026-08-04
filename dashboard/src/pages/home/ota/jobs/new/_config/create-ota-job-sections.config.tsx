/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react";
import type { LucideIcon } from "lucide-react";
import type { TFunction } from "i18next";
import { FileText, Target } from "lucide-react";
import { BasicDetailsSection } from "../_components/basic-details-section";
import { TargetDetailsSection } from "../_components/target-details-section";

export interface CreateOtaJobFormSection {
  id: string;
  Icon: LucideIcon;
  label: string;
  secondaryText: string;
  Content: ComponentType;
}

export function getCreateOtaJobSections(
  t: TFunction,
): CreateOtaJobFormSection[] {
  return [
    {
      id: "basic-details",
      Icon: FileText,
      label: t("createOtaJobPage.sections.basicDetails.label", "Basic details"),
      secondaryText: t(
        "createOtaJobPage.sections.basicDetails.description",
        "Name this job and choose the firmware image to roll out.",
      ),
      Content: BasicDetailsSection,
    },
    {
      id: "target-details",
      Icon: Target,
      label: t(
        "createOtaJobPage.sections.targetDetails.label",
        "Target details",
      ),
      secondaryText: t(
        "createOtaJobPage.sections.targetDetails.description",
        "Choose what this job targets and how it is delivered.",
      ),
      Content: TargetDetailsSection,
    },
  ];
}
