/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { OtaImageTextField } from "../ota-image-text-field";

export function OtherDetailsSection() {
  const { t } = useTranslation("ota-images");

  return (
    <div className="flex flex-col gap-6">
      <OtaImageTextField
        name="model"
        label={t("fields.model.label", "Target model")}
        placeholder={t(
          "fields.model.placeholder",
          "e.g. 3C_DL, WS2812_STRIP, 1SOCK",
        )}
      />
      <OtaImageTextField
        name="platform"
        label={t("fields.platform.label", "Platform")}
        placeholder={t(
          "fields.platform.placeholder",
          "e.g. esp32, esp32c3, esp32s3",
        )}
      />
    </div>
  );
}
