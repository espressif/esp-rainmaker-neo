/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { RainmakerBrandError } from "@/utils/rainmaker-brand/rainmaker-brand";
import { getBrandingErrorMessage } from "../../branding.utils";

/** Turns a failed publish into a translated message that tells the admin what to do next. */
export function getPublishErrorMessage(error: Error, t: TFunction): string {
  if (error instanceof RainmakerBrandError) {
    const detail = error.detail;
    const messages: Record<RainmakerBrandError["reason"], string> = {
      notRainmaker: t(
        "form.errors.notRainmaker",
        "This is not a .rainmaker file saved from App Studio.",
      ),
      noBrand: t(
        "form.errors.noBrand",
        "This file has no app brand. Save the project again in App Studio and upload the new file.",
      ),
      invalidBrand: t(
        "form.errors.invalidBrand",
        "The brand in this file is not valid ({{detail}}).",
        { detail },
      ),
      invalidImage: t(
        "form.errors.invalidImage",
        "An image in this file cannot be used ({{detail}}).",
        { detail },
      ),
      tooLarge: t(
        "form.errors.tooLarge",
        "The brand is larger than 32 KB. Remove some customisation and try again.",
      ),
    };
    return messages[error.reason];
  }
  return getBrandingErrorMessage(
    error,
    t,
    t("form.errors.publishFailed", "Could not publish the brand. Try again."),
  );
}
