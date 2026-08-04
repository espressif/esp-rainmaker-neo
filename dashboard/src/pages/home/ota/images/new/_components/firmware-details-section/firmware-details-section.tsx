/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { OtaImageTextField } from "../ota-image-text-field";

export function FirmwareDetailsSection() {
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
      />
      <OtaImageTextField
        name="version"
        required
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
