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
  countInteger: string;
  countMin: string;
  countMax: string;
}

export function getGenerateNodesFormSchemaMessages(
  t: TFunction,
): GenerateNodesFormSchemaMessages {
  return {
    countInteger: t(
      "errors.countInteger",
      "Enter a whole number of devices.",
    ),
    countMin: t("errors.countMin", "Generate at least 1 device."),
    countMax: t("errors.countMax", "Maximum 20 devices per batch."),
  };
}

export function buildGenerateNodesFormSchema(
  messages: GenerateNodesFormSchemaMessages,
) {
  return z.object({
    count: z
      .number({ invalid_type_error: messages.countInteger })
      .int(messages.countInteger)
      .min(GENERATE_NODES_MIN_COUNT, messages.countMin)
      .max(GENERATE_NODES_MAX_COUNT, messages.countMax),
    matter: z.boolean(),
  });
}

export type GenerateNodesFormValues = z.infer<
  ReturnType<typeof buildGenerateNodesFormSchema>
>;
