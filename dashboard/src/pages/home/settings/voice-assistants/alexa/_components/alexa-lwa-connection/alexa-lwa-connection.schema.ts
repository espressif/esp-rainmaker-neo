/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";
import { LWA_CLIENT_ID_PREFIX } from "../../alexa.constants";

export function buildAlexaLwaConnectionSchema(t: TFunction) {
  return z.object({
    lwa_client_id: z
      .string()
      .trim()
      .min(1, t("alexa.autoCreate.errors.clientIdRequired", "Security Profile Client ID is required."))
      .startsWith(
        LWA_CLIENT_ID_PREFIX,
        t("alexa.autoCreate.errors.clientIdFormat", "Client ID starts with {{prefix}}", { prefix: LWA_CLIENT_ID_PREFIX }),
      ),
    lwa_client_secret: z
      .string()
      .min(1, t("alexa.autoCreate.errors.clientSecretRequired", "Security Profile Client Secret is required.")),
  });
}

export type AlexaLwaConnectionValues = z.infer<ReturnType<typeof buildAlexaLwaConnectionSchema>>;
