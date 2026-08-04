/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";

/** File extensions accepted for an OTA image upload. */
export const OTA_IMAGE_ACCEPTED_EXTENSIONS = [
  ".bin",
  ".elf",
  ".img",
  ".hex",
  ".ota",
] as const;

/** Comma-joined list for the native input `accept` attribute. */
export const OTA_IMAGE_ACCEPT_ATTR = OTA_IMAGE_ACCEPTED_EXTENSIONS.join(",");

/** Firmware name may contain letters, numbers, dots, underscores and hyphens. */
const FIRMWARE_NAME_REGEX = /^[a-zA-Z0-9._-]+$/;

interface UploadOtaImageFormSchemaMessages {
  firmwareFileRequired: string;
  firmwareFileInvalidType: string;
  nameRequired: string;
  nameInvalid: string;
  versionRequired: string;
}

export function getUploadOtaImageFormSchemaMessages(
  t: TFunction,
): UploadOtaImageFormSchemaMessages {
  return {
    firmwareFileRequired: t(
      "errors.firmwareFileRequired",
      "Please select a firmware file.",
    ),
    firmwareFileInvalidType: t(
      "errors.firmwareFileInvalidType",
      "Supported file types: .bin, .elf, .img, .hex, .ota.",
    ),
    nameRequired: t(
      "errors.nameRequired",
      "Firmware name is required.",
    ),
    nameInvalid: t(
      "errors.nameInvalid",
      "Only letters, numbers, dots, underscores and hyphens are allowed.",
    ),
    versionRequired: t(
      "errors.versionRequired",
      "Firmware version is required.",
    ),
  };
}

export function buildUploadOtaImageFormSchema(
  messages: UploadOtaImageFormSchemaMessages,
) {
  return z.object({
    firmwareFiles: z
      .array(z.instanceof(File))
      .min(1, messages.firmwareFileRequired)
      .superRefine((files, ctx) => {
        const file = files[0];
        if (!file) {
          return;
        }
        const isAccepted = OTA_IMAGE_ACCEPTED_EXTENSIONS.some((ext) =>
          file.name.toLowerCase().endsWith(ext),
        );
        if (!isAccepted) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: messages.firmwareFileInvalidType,
          });
        }
      }),
    name: z
      .string()
      .min(1, messages.nameRequired)
      .regex(FIRMWARE_NAME_REGEX, messages.nameInvalid),
    version: z.string().min(1, messages.versionRequired),
    type: z.string().optional(),
    model: z.string().optional(),
    platform: z.string().optional(),
  });
}

export type UploadOtaImageFormValues = z.infer<
  ReturnType<typeof buildUploadOtaImageFormSchema>
>;
