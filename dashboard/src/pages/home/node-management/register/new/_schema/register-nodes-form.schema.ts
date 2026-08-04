/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";

export const REGISTER_NODES_CAPABILITY_IDS = ["s3", "kvs", "bridge"] as const;

export type RegisterNodesCapability =
  (typeof REGISTER_NODES_CAPABILITY_IDS)[number];

const TAG_REGEX = /^[^:\s]+:[^:\s]+$/;

interface RegisterNodesFormSchemaMessages {
  certificateFileRequired: string;
  certificateFileCsvOnly: string;
}

export function getRegisterNodesFormSchemaMessages(
  t: TFunction,
): RegisterNodesFormSchemaMessages {
  return {
    certificateFileRequired: t(
      "new.errors.certificateFileRequired",
      "Please upload a node certificate CSV.",
    ),
    certificateFileCsvOnly: t(
      "new.errors.certificateFileCsvOnly",
      "Only CSV files are supported.",
    ),
  };
}

export function buildRegisterNodesFormSchema(
  messages: RegisterNodesFormSchemaMessages,
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
    capabilities: z.array(z.enum(REGISTER_NODES_CAPABILITY_IDS)),
    tags: z.array(z.string().regex(TAG_REGEX)),
  });
}

export type RegisterNodesFormValues = z.infer<
  ReturnType<typeof buildRegisterNodesFormSchema>
>;
