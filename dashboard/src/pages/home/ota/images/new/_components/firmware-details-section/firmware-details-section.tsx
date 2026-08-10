/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { OtaImageTextField } from "../ota-image-text-field";
import type { UploadOtaImageSectionContentProps } from "../../_config/upload-ota-image-sections.config";

export function FirmwareDetailsSection({
  lockedFields,
}: UploadOtaImageSectionContentProps) {
  const { t } = useTranslation("ota-images");

  return (
    <div className="flex flex-col gap-6">
      <OtaImageTextField
        name="name"
        required
        label={t("fields.name.label", "Firmware name")}
        placeholder={t(
          "fields.name.placeholder",
          "e.g. light-sensor",
        )}
        tooltip={t(
          "fields.name.tooltip",
          "We recommend a unique name — this is how the image is identified when you create an OTA job.",
        )}
      />
      <OtaImageTextField
        name="version"
        required
        locked={lockedFields.has("version")}
        label={t("fields.version.label", "Firmware version")}
        placeholder={t(
          "fields.version.placeholder",
          "e.g. 1.0.0",
        )}
      />
      <OtaImageTextField
        name="type"
        label={t("fields.type.label", "Firmware type")}
        placeholder={t(
          "fields.type.placeholder",
          "e.g. Light, Switch",
        )}
      />
    </div>
  );
}
