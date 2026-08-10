/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";

export const GENERATE_NODES_MIN_COUNT = 1;
export const GENERATE_NODES_MAX_COUNT = 20;

interface GenerateNodesFormSchemaMessages {
  countRequired: string;
  countInteger: string;
  countMin: string;
  countMax: string;
}

export function getGenerateNodesFormSchemaMessages(
  t: TFunction,
): GenerateNodesFormSchemaMessages {
  return {
    countRequired: t(
      "errors.countRequired",
      "Enter the number of nodes to generate.",
    ),
    countInteger: t(
      "errors.countInteger",
      "Enter a whole number of nodes.",
    ),
    countMin: t("errors.countMin", "Generate at least 1 node."),
    countMax: t("errors.countMax", "Maximum 20 nodes per batch."),
  };
}

export function buildGenerateNodesFormSchema(
  messages: GenerateNodesFormSchemaMessages,
) {
  return z.object({
    // Value is held as a string in form state so the field can go transiently
    // empty while the user is editing; validation runs on submit.
    count: z.string().superRefine((raw, ctx) => {
      const trimmed = raw.trim();
      if (!trimmed) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: messages.countRequired,
        });
        return;
      }
      if (!/^\d+$/.test(trimmed)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: messages.countInteger,
        });
        return;
      }
      const parsed = Number.parseInt(trimmed, 10);
      if (parsed < GENERATE_NODES_MIN_COUNT) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: messages.countMin,
        });
        return;
      }
      if (parsed > GENERATE_NODES_MAX_COUNT) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: messages.countMax,
        });
      }
    }),
    matter: z.boolean(),
  });
}

export type GenerateNodesFormValues = z.infer<
  ReturnType<typeof buildGenerateNodesFormSchema>
>;
