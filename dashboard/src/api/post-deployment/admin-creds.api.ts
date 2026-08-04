/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { httpClient } from "@/api/http-client";
import { ApiError } from "@/api/api.errors";
import { sigv4Request } from "@/api/sigv4-client";
import { getApiGatewayUrl } from "@/lib/config";
import type { AdminCredsResponse, ScopedAwsCredentials } from "./post-deployment.types";

const ENDPOINT = "/v1/admin/credentials";

function toScoped(res: AdminCredsResponse): ScopedAwsCredentials {
  return {
    accessKeyId: res.access_key,
    secretAccessKey: res.secret_key,
    sessionToken: res.session_token,
    expiration: res.expiration ?? null,
  };
}

/**
 * espuser-scoped credentials (SES and SMS sandbox state, SMS sandbox numbers). Goes through
 * `httpClient`,
 * which targets the ESP User API and attaches the admin Bearer id token the Cognito
 * authorizer expects — so an expired token is refreshed rather than surfacing here.
 */
export async function fetchEspUserAdminCreds(): Promise<ScopedAwsCredentials> {
  try {
    const res = await httpClient.post<AdminCredsResponse>(ENDPOINT, {});
    return toScoped(res.data);
  } catch (error) {
    throw ApiError.fromAxiosError(error);
  }
}

/**
 * rmng-scoped credentials (the Lambda concurrency limit). The rmng admin-creds
 * endpoint is AWS_IAM, so we SigV4-sign the call with the dashboard's baseline
 * identity-pool credentials — mirroring iot-event-mode.
 */
export async function fetchRmngAdminCreds(): Promise<ScopedAwsCredentials> {
  if (!getApiGatewayUrl()) {
    throw new Error("API Gateway URL is not configured");
  }
  const res = await sigv4Request<AdminCredsResponse>({
    method: "POST",
    url: `${getApiGatewayUrl().replace(/\/$/, "")}${ENDPOINT}`,
    body: {},
  });
  return toScoped(res);
}
