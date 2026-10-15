/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";

const TAG_REGEX = /^[^:\s]+:[^:\s]+$/;

interface UpdateNodesFormSchemaMessages {
  certificateFileRequired: string;
  certificateFileCsvOnly: string;
}

export function getUpdateNodesFormSchemaMessages(
  t: TFunction,
): UpdateNodesFormSchemaMessages {
  return {
    certificateFileRequired: t(
      "new.errors.certificateFileRequired",
      "Please upload a node CSV.",
    ),
    certificateFileCsvOnly: t(
      "new.errors.certificateFileCsvOnly",
      "Only CSV files are supported.",
    ),
  };
}

export function buildUpdateNodesFormSchema(
  messages: UpdateNodesFormSchemaMessages,
) {
  return z.object({
    certificateFiles: z
      .array(z.instanceof(File))
      .min(1, messages.certificateFileRequired)
      .superRefine((files, ctx) => {
        const file = files[0];
        if (file && !file.name.toLowerCase().endsWith(".csv")) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: messages.certificateFileCsvOnly,
          });
        }
      }),
    groupName: z.string().optional(),
    subgroupName: z.string().optional(),
    tags: z.array(z.string().regex(TAG_REGEX)),
  });
}

export type UpdateNodesFormValues = z.infer<
  ReturnType<typeof buildUpdateNodesFormSchema>
>;
