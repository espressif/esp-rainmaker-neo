/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type {
  GvaServiceAccount,
  PushIntegrationRequest,
  PushIntegrationType,
} from "@/api/integrations";
import type { PushIntegrationFormValues } from "./push-integration-form.schema";

/**
 * Shape consumed by `useRegisterPushIntegration()`: the concrete backend
 * `integration_type` plus its credential body.
 */
export interface PushIntegrationRegisterPayload {
  integrationType: PushIntegrationType;
  data: PushIntegrationRequest;
}

/** Tags a service-account file failure so the caller can pick the right message. */
export type ServiceAccountParseError = "invalid-json" | "invalid-format";

export class ServiceAccountFileError extends Error {
  constructor(readonly reason: ServiceAccountParseError) {
    super(reason);
    this.name = "ServiceAccountFileError";
  }
}

/**
 * Map the iOS (APNS) branch of the form to a register payload. `sandbox`
 * selects the development platform; it is not part of the credential body.
 */
export function buildIosRegisterPayload(
  values: PushIntegrationFormValues,
): PushIntegrationRegisterPayload {
  return {
    integrationType: values.sandbox ? "apns_sandbox" : "apns",
    data: {
      bundle_id: values.bundle_id.trim(),
      key_id: values.key_id.trim(),
      team_id: values.team_id.trim(),
      authentication_key: values.authentication_key.trim(),
    },
  };
}

/**
 * Read + validate an uploaded Google service-account JSON file into the GCM
 * register body. Throws {@link ServiceAccountFileError} on unparseable JSON or
 * when the mandatory fields are missing, so the caller can surface a
 * field-level message.
 */
export async function parseServiceAccountFile(
  file: File,
): Promise<GvaServiceAccount> {
  const rawText = await file.text();

  let parsed: Partial<GvaServiceAccount>;
  try {
    parsed = JSON.parse(rawText) as Partial<GvaServiceAccount>;
  } catch {
    throw new ServiceAccountFileError("invalid-json");
  }

  if (!parsed.project_id || !parsed.client_email || !parsed.private_key) {
    throw new ServiceAccountFileError("invalid-format");
  }

  return parsed as GvaServiceAccount;
}
