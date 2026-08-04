/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";

export interface PushIntegrationFormSchemaMessages {
  bundleIdRequired: string;
  keyIdRequired: string;
  teamIdRequired: string;
  authKeyRequired: string;
  serviceAccountRequired: string;
}

export function getPushIntegrationFormSchemaMessages(
  t: TFunction,
): PushIntegrationFormSchemaMessages {
  return {
    bundleIdRequired: t(
      "form.errors.bundleIdRequired",
      "Bundle ID is required.",
    ),
    keyIdRequired: t("form.errors.keyIdRequired", "Key ID is required."),
    teamIdRequired: t("form.errors.teamIdRequired", "Team ID is required."),
    authKeyRequired: t(
      "form.errors.authKeyRequired",
      "Authentication key is required.",
    ),
    serviceAccountRequired: t(
      "form.errors.serviceAccountRequired",
      "Upload a service account JSON file.",
    ),
  };
}

/**
 * A single schema drives both platforms. Fields are always present so the form
 * keeps a stable shape; `superRefine` only enforces the subset relevant to the
 * chosen `integration_type`, so hidden-branch fields never block submission.
 */
export function buildPushIntegrationFormSchema(
  messages: PushIntegrationFormSchemaMessages,
) {
  return z
    .object({
      integration_type: z.enum(["ios", "android"]),
      bundle_id: z.string(),
      key_id: z.string(),
      team_id: z.string(),
      authentication_key: z.string(),
      sandbox: z.boolean(),
      service_account: z.array(z.instanceof(File)),
    })
    .superRefine((values, ctx) => {
      if (values.integration_type === "ios") {
        if (!values.bundle_id.trim()) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: messages.bundleIdRequired,
            path: ["bundle_id"],
          });
        }
        if (!values.key_id.trim()) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: messages.keyIdRequired,
            path: ["key_id"],
          });
        }
        if (!values.team_id.trim()) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: messages.teamIdRequired,
            path: ["team_id"],
          });
        }
        if (!values.authentication_key.trim()) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: messages.authKeyRequired,
            path: ["authentication_key"],
          });
        }
        return;
      }

      if (values.service_account.length === 0) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: messages.serviceAccountRequired,
          path: ["service_account"],
        });
      }
    });
}

export type PushIntegrationFormValues = z.infer<
  ReturnType<typeof buildPushIntegrationFormSchema>
>;

export const PUSH_INTEGRATION_FORM_DEFAULTS: PushIntegrationFormValues = {
  integration_type: "ios",
  bundle_id: "",
  key_id: "",
  team_id: "",
  authentication_key: "",
  sandbox: false,
  service_account: [],
};
