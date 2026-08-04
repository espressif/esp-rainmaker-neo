/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";
import {
  GROUP_NAME_MAX_LENGTH,
  GROUP_NAME_REGEX,
} from "../_constants/create-node-group-form.constants";

interface CreateNodeGroupFormSchemaMessages {
  nameRequired: string;
  nameTooLong: string;
  nameInvalid: string;
  parentRequired: string;
  rulesRequired: string;
}

export function getCreateNodeGroupFormSchemaMessages(
  t: TFunction,
): CreateNodeGroupFormSchemaMessages {
  return {
    nameRequired: t("new.errors.nameRequired", "Group name is required."),
    nameTooLong: t(
      "new.errors.nameTooLong",
      "Group name must be at most 128 characters.",
    ),
    nameInvalid: t(
      "new.errors.nameInvalid",
      "Group name can only contain letters, numbers, colons, underscores and hyphens.",
    ),
    parentRequired: t(
      "new.errors.parentRequired",
      "Select a parent group for this sub-group.",
    ),
    rulesRequired: t(
      "new.errors.rulesRequired",
      "Add at least one rule for a dynamic group.",
    ),
  };
}

const queryRuleSchema = z.object({
  field: z.string().min(1),
  value: z.string().min(1),
});

export function buildCreateNodeGroupFormSchema(
  messages: CreateNodeGroupFormSchemaMessages,
) {
  return z
    .object({
      groupName: z
        .string()
        .min(1, messages.nameRequired)
        .max(GROUP_NAME_MAX_LENGTH, messages.nameTooLong)
        .regex(GROUP_NAME_REGEX, messages.nameInvalid),
      description: z.string().optional(),
      createAsSubgroup: z.boolean(),
      parentGroupName: z.string(),
      createAsDynamic: z.boolean(),
      queryRules: z.array(queryRuleSchema),
    })
    .superRefine((values, ctx) => {
      if (values.createAsSubgroup && !values.parentGroupName.trim()) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: messages.parentRequired,
          path: ["parentGroupName"],
        });
      }
      if (values.createAsDynamic && values.queryRules.length === 0) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: messages.rulesRequired,
          path: ["queryRules"],
        });
      }
    });
}

export type CreateNodeGroupFormValues = z.infer<
  ReturnType<typeof buildCreateNodeGroupFormSchema>
>;
