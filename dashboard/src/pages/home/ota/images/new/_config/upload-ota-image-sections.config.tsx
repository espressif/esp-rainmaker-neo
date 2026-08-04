/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react";
import type { LucideIcon } from "lucide-react";
import type { TFunction } from "i18next";
import { FileUp, FileText, Settings2 } from "lucide-react";
import { SelectOtaImageSection } from "../_components/select-ota-image-section";
import { FirmwareDetailsSection } from "../_components/firmware-details-section";
import { OtherDetailsSection } from "../_components/other-details-section";

export interface UploadOtaImageFormSection {
  id: string;
  Icon: LucideIcon;
  label: string;
  Content: ComponentType;
}

export function getUploadOtaImageSections(
  t: TFunction,
): UploadOtaImageFormSection[] {
  return [
    {
      id: "select-image",
      Icon: FileUp,
      label: t("sections.selectImage", "Select OTA Image"),
      Content: SelectOtaImageSection,
    },
    {
      id: "firmware-details",
      Icon: FileText,
      label: t("sections.firmwareDetails", "Add firmware details"),
      Content: FirmwareDetailsSection,
    },
    {
      id: "other-details",
      Icon: Settings2,
      label: t("sections.otherDetails", "Other details"),
      Content: OtherDetailsSection,
    },
  ];
}
