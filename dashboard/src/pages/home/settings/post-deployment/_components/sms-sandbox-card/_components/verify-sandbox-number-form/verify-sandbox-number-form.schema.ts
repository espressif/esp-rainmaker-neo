/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";

/** SNS one-time passwords are always six digits. */
const OTP_PATTERN = /^\d{6}$/;

export interface VerifySandboxNumberFormSchemaMessages {
  invalidOneTimePassword: string;
}

export function getVerifySandboxNumberFormSchemaMessages(
  t: TFunction,
): VerifySandboxNumberFormSchemaMessages {
  return {
    invalidOneTimePassword: t("smsSandbox.errors.invalidOneTimePassword", "Enter the 6-digit code from the SMS."),
  };
}

export function buildVerifySandboxNumberFormSchema(
  messages: VerifySandboxNumberFormSchemaMessages,
) {
  return z.object({
    one_time_password: z
      .string()
      .trim()
      .regex(OTP_PATTERN, messages.invalidOneTimePassword),
  });
}

export type VerifySandboxNumberFormValues = z.infer<
  ReturnType<typeof buildVerifySandboxNumberFormSchema>
>;

export const VERIFY_SANDBOX_NUMBER_FORM_DEFAULTS: VerifySandboxNumberFormValues = {
  one_time_password: "",
};
