/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";

export const RAINMAKER_FILE_ACCEPT_ATTR = ".rainmaker";

export interface BrandingFormSchemaMessages {
  fileRequired: string;
}

export function getBrandingFormSchemaMessages(
  t: TFunction,
): BrandingFormSchemaMessages {
  return {
    fileRequired: t(
      "form.errors.fileRequired",
      "Choose a .rainmaker file saved from App Studio.",
    ),
  };
}

export function buildBrandingFormSchema(messages: BrandingFormSchemaMessages) {
  return z.object({
    rainmakerFiles: z.array(z.instanceof(File)).min(1, messages.fileRequired),
  });
}

export type BrandingFormValues = z.infer<
  ReturnType<typeof buildBrandingFormSchema>
>;

export const BRANDING_FORM_DEFAULTS: BrandingFormValues = {
  rainmakerFiles: [],
};
