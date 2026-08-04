/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";
import {
  JOB_MODE_CONTINUOUS,
  JOB_MODE_SNAPSHOT,
  OTA_JOB_NAME_MAX_LENGTH,
  OTA_JOB_NAME_REGEX,
  TARGET_TYPE_GROUP,
  TARGET_TYPE_NODE,
} from "../_constants/create-ota-job-form.constants";

interface CreateOtaJobFormSchemaMessages {
  nameRequired: string;
  nameTooLong: string;
  nameInvalid: string;
  firmwareRequired: string;
  targetRequired: string;
}

export function getCreateOtaJobFormSchemaMessages(
  t: TFunction,
): CreateOtaJobFormSchemaMessages {
  return {
    nameRequired: t(
      "createOtaJobPage.errors.nameRequired",
      "Name is required.",
    ),
    nameTooLong: t(
      "createOtaJobPage.errors.nameTooLong",
      "Name must be at most 128 characters.",
    ),
    nameInvalid: t(
      "createOtaJobPage.errors.nameInvalid",
      "Name can only contain letters, numbers, underscores and hyphens.",
    ),
    firmwareRequired: t(
      "createOtaJobPage.errors.firmwareRequired",
      "Firmware image is required.",
    ),
    targetRequired: t(
      "createOtaJobPage.errors.targetRequired",
      "Target is required.",
    ),
  };
}

const queryRuleSchema = z.object({
  field: z.string().min(1),
  value: z.string().min(1),
});

export function buildCreateOtaJobFormSchema(
  messages: CreateOtaJobFormSchemaMessages,
) {
  return z
    .object({
      name: z
        .string()
        .min(1, messages.nameRequired)
        .max(OTA_JOB_NAME_MAX_LENGTH, messages.nameTooLong)
        .regex(OTA_JOB_NAME_REGEX, messages.nameInvalid),
      firmwareKey: z.string().min(1, messages.firmwareRequired),
      // Captured from the selected image's S3 ETag (machine-set, not user input).
      // Passed through as the OTA job's file_md5 to enable download resume +
      // integrity check; the service drops it when it is not a plain MD5.
      fileMd5: z.string().optional(),
      targetType: z.enum([TARGET_TYPE_GROUP, TARGET_TYPE_NODE]),
      targetSelection: z.enum([JOB_MODE_SNAPSHOT, JOB_MODE_CONTINUOUS]),
      source: z.string(),
      targetName: z.string().optional(),
      queryRules: z.array(queryRuleSchema),
    })
    .refine(
      (values) => {
        if (values.queryRules.length > 0) {
          return true;
        }
        return !!values.targetName;
      },
      { message: messages.targetRequired, path: ["targetName"] },
    );
}

export type CreateOtaJobFormValues = z.infer<
  ReturnType<typeof buildCreateOtaJobFormSchema>
>;

export type CreateOtaJobQueryRule = z.infer<typeof queryRuleSchema>;
