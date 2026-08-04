/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** Which stack owns a post-deployment step (which creds API vends its creds). */
export type StepStack = "espuser" | "rmng";

/** Raw shape returned by both admin-creds endpoints. */
export interface AdminCredsResponse {
  access_key: string;
  secret_key: string;
  session_token: string;
  expiration: number | null;
}

/** Normalized scoped credentials used to build AWS SDK clients in the browser. */
export interface ScopedAwsCredentials {
  accessKeyId: string;
  secretAccessKey: string;
  sessionToken: string;
  expiration: number | null;
}
