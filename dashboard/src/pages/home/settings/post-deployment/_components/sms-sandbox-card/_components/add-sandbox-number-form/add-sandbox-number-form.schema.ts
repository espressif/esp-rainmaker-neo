/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";

/** E.164: a leading `+`, a non-zero country code, and 8–15 digits in total. */
const E164_PATTERN = /^\+[1-9]\d{7,14}$/;

export interface AddSandboxNumberFormSchemaMessages {
  invalidPhoneNumber: string;
}

export function getAddSandboxNumberFormSchemaMessages(
  t: TFunction,
): AddSandboxNumberFormSchemaMessages {
  return {
    invalidPhoneNumber: t("smsSandbox.errors.invalidPhoneNumber", "Enter the number in E.164 format, for example +15551234567."),
  };
}

export function buildAddSandboxNumberFormSchema(
  messages: AddSandboxNumberFormSchemaMessages,
) {
  return z.object({
    phone_number: z
      .string()
      .trim()
      .regex(E164_PATTERN, messages.invalidPhoneNumber),
  });
}

export type AddSandboxNumberFormValues = z.infer<
  ReturnType<typeof buildAddSandboxNumberFormSchema>
>;

export const ADD_SANDBOX_NUMBER_FORM_DEFAULTS: AddSandboxNumberFormValues = {
  phone_number: "",
};
