/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { z } from "zod";
import type {
  GvaConfigGetResponse,
  GvaConfigRequest,
} from "@/api/integrations";

export interface GvaConfigFormSchemaMessages {
  projectIdRequired: string;
  privateKeyRequired: string;
  clientEmailRequired: string;
  clientEmailInvalid: string;
  urlInvalid: string;
}

export function getGvaConfigFormSchemaMessages(
  t: TFunction,
): GvaConfigFormSchemaMessages {
  return {
    projectIdRequired: t("gva.form.errors.projectIdRequired", "Project ID is required."),
    privateKeyRequired: t(
      "gva.form.errors.privateKeyRequired",
      "Private Key is required.",
    ),
    clientEmailRequired: t(
      "gva.form.errors.clientEmailRequired",
      "Client Email is required.",
    ),
    clientEmailInvalid: t(
      "gva.form.errors.clientEmailInvalid",
      "Must be a valid email address.",
    ),
    urlInvalid: t("gva.form.errors.urlInvalid", "Must be a valid URL."),
  };
}

/** Optional URL: an empty string is allowed; a non-empty value must be a URL. */
function optionalUrl(message: string) {
  return z.union([z.literal(""), z.string().trim().url(message)]).optional();
}

/**
 * Single schema used both as the form resolver and to validate an uploaded
 * service-account JSON. Only `project_id`, `private_key` and `client_email` are
 * required (matching the legacy GVA behaviour); every other field is optional,
 * and the URL fields are validated only when non-empty. `redirect_uris` is
 * intentionally absent — it is server-computed and never submitted.
 */
export function buildGvaConfigFormSchema(messages: GvaConfigFormSchemaMessages) {
  return z.object({
    type: z.string().trim().optional(),
    project_id: z.string().trim().min(1, messages.projectIdRequired),
    private_key_id: z.string().trim().optional(),
    private_key: z.string().min(1, messages.privateKeyRequired),
    client_email: z
      .string()
      .trim()
      .min(1, messages.clientEmailRequired)
      .email(messages.clientEmailInvalid),
    client_id: z.string().trim().optional(),
    auth_uri: optionalUrl(messages.urlInvalid),
    token_uri: optionalUrl(messages.urlInvalid),
    auth_provider_x509_cert_url: optionalUrl(messages.urlInvalid),
    client_x509_cert_url: optionalUrl(messages.urlInvalid),
    universe_domain: z.string().trim().optional(),
  });
}

export type GvaConfigFormValues = z.infer<
  ReturnType<typeof buildGvaConfigFormSchema>
>;

const GVA_FORM_DEFAULTS: GvaConfigFormValues = {
  type: "service_account",
  project_id: "",
  private_key_id: "",
  private_key: "",
  client_email: "",
  client_id: "",
  auth_uri: "",
  token_uri: "",
  auth_provider_x509_cert_url: "",
  client_x509_cert_url: "",
  universe_domain: "",
};

/**
 * Seed the form from the fetched config. The backend never returns the write-only `private_key` (M-13), so it always seeds empty and the admin must re-provide it (paste or re-upload) when editing.
 */
export function buildGvaConfigFormDefaults(
  initialData?: GvaConfigGetResponse,
): GvaConfigFormValues {
  if (!initialData) {
    return { ...GVA_FORM_DEFAULTS };
  }
  return {
    type: initialData.type ?? GVA_FORM_DEFAULTS.type,
    project_id: initialData.project_id ?? "",
    private_key_id: initialData.private_key_id ?? "",
    private_key: "",
    client_email: initialData.client_email ?? "",
    client_id: initialData.client_id ?? "",
    auth_uri: initialData.auth_uri ?? "",
    token_uri: initialData.token_uri ?? "",
    auth_provider_x509_cert_url: initialData.auth_provider_x509_cert_url ?? "",
    client_x509_cert_url: initialData.client_x509_cert_url ?? "",
    universe_domain: initialData.universe_domain ?? "",
  };
}

const OPTIONAL_PAYLOAD_KEYS = [
  "type",
  "private_key_id",
  "client_id",
  "auth_uri",
  "token_uri",
  "auth_provider_x509_cert_url",
  "client_x509_cert_url",
  "universe_domain",
] as const;

/**
 * Build the POST body, dropping empty optional fields so the request carries
 * only the credentials actually provided (the required three plus any non-empty
 * optionals) — mirroring the partial payload the legacy form submitted.
 */
export function buildGvaConfigPayload(
  values: GvaConfigFormValues,
): GvaConfigRequest {
  const payload: GvaConfigRequest = {
    project_id: values.project_id,
    private_key: values.private_key,
    client_email: values.client_email,
  };
  for (const key of OPTIONAL_PAYLOAD_KEYS) {
    const value = values[key];
    if (value && value.trim() !== "") {
      payload[key] = value;
    }
  }
  return payload;
}
