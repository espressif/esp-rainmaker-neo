/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * i18n key per AWS failure on a *read* path, keyed on the code the SDK reports as
 * `Error.name`. Narrower than the write-path table in
 * {@link ./_components/sms-sandbox-card/sms-sandbox-card.utils.ts}: reading a limit
 * cannot be throttled by opt-outs or hit the sandbox number cap, so only the
 * session and permission failures are worth distinct copy.
 */
const READ_ERROR_MESSAGE_KEYS: Record<string, string> = {
  // The vended session is dead rather than the request being wrong — a redeploy that
  // replaces the credentials role invalidates every session it issued, and the
  // cached ones outlive it.
  ExpiredToken: "awsErrors.staleCredentials",
  ExpiredTokenException: "awsErrors.staleCredentials",
  InvalidClientTokenId: "awsErrors.staleCredentials",
  UnrecognizedClientException: "awsErrors.staleCredentials",
  AuthorizationErrorException: "awsErrors.notPermitted",
  AccessDeniedException: "awsErrors.notPermitted",
  ThrottledException: "awsErrors.throttled",
  TooManyRequestsException: "awsErrors.throttled",
};

export const AWS_READ_ERROR_FALLBACK_KEY = "awsErrors.generic";

/**
 * Maps a failed limit read to a translation key. Raw SDK messages are never
 * surfaced: they name internal parameters and read as AWS jargon.
 */
export function awsReadErrorMessageKey(error: unknown): string {
  const code = error instanceof Error ? error.name : "";
  return READ_ERROR_MESSAGE_KEYS[code] ?? AWS_READ_ERROR_FALLBACK_KEY;
}
