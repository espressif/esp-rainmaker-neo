/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";
import type {
  SmartThingsConfigGetResponse,
  SmartThingsConfigRequest,
} from "@/api/integrations";

/** Mirrors the server-side bounds in st_cfg_main.go. */
const CLIENT_ID_MAX_LENGTH = 256;
const CLIENT_SECRET_MAX_LENGTH = 1024;

export interface SmartThingsConfigFormSchemaMessages {
  clientIdRequired: string;
  clientIdTooLong: string;
  clientSecretRequired: string;
  clientSecretTooLong: string;
}

export function getSmartThingsConfigFormSchemaMessages(
  t: TFunction,
): SmartThingsConfigFormSchemaMessages {
  return {
    clientIdRequired: t(
      "smartthings.form.errors.clientIdRequired",
      "Client ID is required.",
    ),
    clientIdTooLong: t(
      "smartthings.form.errors.clientIdTooLong",
      "Client ID must be at most 256 characters.",
    ),
    clientSecretRequired: t(
      "smartthings.form.errors.clientSecretRequired",
      "Client Secret is required.",
    ),
    clientSecretTooLong: t(
      "smartthings.form.errors.clientSecretTooLong",
      "Client Secret must be at most 1024 characters.",
    ),
  };
}

export function buildSmartThingsConfigFormSchema(
  messages: SmartThingsConfigFormSchemaMessages,
) {
  return z.object({
    client_id: z
      .string()
      .trim()
      .min(1, messages.clientIdRequired)
      .max(CLIENT_ID_MAX_LENGTH, messages.clientIdTooLong),
    client_secret: z
      .string()
      .trim()
      .min(1, messages.clientSecretRequired)
      .max(CLIENT_SECRET_MAX_LENGTH, messages.clientSecretTooLong),
  });
}

export type SmartThingsConfigFormValues = z.infer<
  ReturnType<typeof buildSmartThingsConfigFormSchema>
>;

/** The secret is write-only: GET returns the client ID alone, so it starts empty. */
export function buildSmartThingsConfigFormDefaults(
  initialData?: SmartThingsConfigGetResponse,
): SmartThingsConfigFormValues {
  return {
    client_id: initialData?.client_id ?? "",
    client_secret: "",
  };
}

export function buildSmartThingsConfigPayload(
  values: SmartThingsConfigFormValues,
): SmartThingsConfigRequest {
  return {
    client_id: values.client_id,
    client_secret: values.client_secret,
  };
}
