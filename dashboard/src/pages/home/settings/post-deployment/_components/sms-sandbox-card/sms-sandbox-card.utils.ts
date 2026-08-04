/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** i18n key per SNS API error code, keyed on the code the SDK reports as `Error.name`. */
const ERROR_MESSAGE_KEYS: Record<string, string> = {
  OptedOutException: "smsSandbox.errors.optedOut",
  ThrottledException: "smsSandbox.errors.throttled",
  TooManyRequestsException: "smsSandbox.errors.throttled",
  VerificationException: "smsSandbox.errors.verification",
  ResourceNotFoundException: "smsSandbox.errors.notFound",
  LimitExceededException: "smsSandbox.errors.limitExceeded",
  // The vended session is dead rather than the request being wrong — a redeploy that replaces the
  // credentials role invalidates every session it issued, and the cached ones outlive it.
  ExpiredToken: "smsSandbox.errors.staleCredentials",
  ExpiredTokenException: "smsSandbox.errors.staleCredentials",
  InvalidClientTokenId: "smsSandbox.errors.staleCredentials",
  UnrecognizedClientException: "smsSandbox.errors.staleCredentials",
  AuthorizationErrorException: "smsSandbox.errors.notPermitted",
  AccessDeniedException: "smsSandbox.errors.notPermitted",
};

/**
 * Maps an SNS failure to a translation key. Raw SDK messages are never surfaced: they name
 * internal parameters and read as AWS jargon, so anything unrecognized falls back to generic copy.
 */
export function snsErrorMessageKey(error: unknown): string {
  const code = error instanceof Error ? error.name : "";
  return ERROR_MESSAGE_KEYS[code] ?? "smsSandbox.errors.generic";
}
