/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";
import { DEFAULT_ALEXA_MANUFACTURER_NAME } from "../../alexa.constants";

export interface AlexaCreateSkillFormSchemaMessages {
  skillNameRequired: string;
}

export function getAlexaCreateSkillFormSchemaMessages(
  t: TFunction,
): AlexaCreateSkillFormSchemaMessages {
  return {
    skillNameRequired: t(
      "alexa.autoCreate.errors.skillNameRequired",
      "Skill name is required.",
    ),
  };
}

/**
 * Only the skill itself: the backend signs in to SMAPI as the connected Amazon account
 * (see alexa-lwa-connection) and derives the redirect URIs from its vendor id.
 */
export function buildAlexaCreateSkillFormSchema(
  messages: AlexaCreateSkillFormSchemaMessages,
) {
  return z.object({
    skill_name: z.string().trim().min(1, messages.skillNameRequired),
    manufacturer_name: z.string().trim(),
  });
}

export type AlexaCreateSkillFormValues = z.infer<
  ReturnType<typeof buildAlexaCreateSkillFormSchema>
>;

export function buildAlexaCreateSkillFormDefaults(): AlexaCreateSkillFormValues {
  return {
    skill_name: "",
    manufacturer_name: DEFAULT_ALEXA_MANUFACTURER_NAME,
  };
}
