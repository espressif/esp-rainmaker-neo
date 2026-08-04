/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";
import type { AlexaConfigGetResponse } from "@/api/integrations";

export interface AlexaConfigFormSchemaMessages {
  clientIdRequired: string;
  clientSecretRequired: string;
  skillIdRequired: string;
  redirectUriRequired: string;
  redirectUrisRequired: string;
}

export function getAlexaConfigFormSchemaMessages(
  t: TFunction,
): AlexaConfigFormSchemaMessages {
  return {
    clientIdRequired: t(
      "alexa.form.errors.clientIdRequired",
      "Client ID is required.",
    ),
    clientSecretRequired: t(
      "alexa.form.errors.clientSecretRequired",
      "Client Secret is required.",
    ),
    skillIdRequired: t("alexa.form.errors.skillIdRequired", "Skill ID is required."),
    redirectUriRequired: t(
      "alexa.form.errors.redirectUriRequired",
      "Redirect URI cannot be empty.",
    ),
    redirectUrisRequired: t(
      "alexa.form.errors.redirectUrisRequired",
      "Add at least one redirect URI.",
    ),
  };
}

export function buildAlexaConfigFormSchema(
  messages: AlexaConfigFormSchemaMessages,
) {
  return z.object({
    client_id: z.string().trim().min(1, messages.clientIdRequired),
    client_secret: z.string().min(1, messages.clientSecretRequired),
    skill_id: z.string().trim().min(1, messages.skillIdRequired),
    manufacturer_name: z.string().trim(),
    redirect_uris: z
      .array(z.object({ value: z.string().trim().min(1, messages.redirectUriRequired) }))
      .min(1, messages.redirectUrisRequired),
  });
}

export type AlexaConfigFormValues = z.infer<
  ReturnType<typeof buildAlexaConfigFormSchema>
>;

export function buildAlexaConfigFormDefaults(
  initialData?: AlexaConfigGetResponse,
): AlexaConfigFormValues {
  return {
    client_id: initialData?.client_id ?? "",
    client_secret: "",
    skill_id: initialData?.skill_id ?? "",
    manufacturer_name: initialData?.manufacturer_name ?? "",
    redirect_uris: (initialData?.redirect_uris ?? []).map((uri) => ({
      value: uri,
    })),
  };
}
