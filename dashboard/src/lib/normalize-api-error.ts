/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Turn a thrown API error into a user-facing message.
 *
 * The SigV4 client throws `Error("Request failed with status code {status}:
 * {body}")`, where `{body}` is usually the JSON error envelope the backend
 * returns (`{ "description": "..." }`). We lift that human `description` when
 * present; otherwise we fall back to a translated, generic message so raw
 * backend/internal text is never shown to the user.
 */
/**
 * True when a thrown API error is a 404. The SigV4 client encodes the HTTP
 * status into the `Error` message (`"...status code 404..."`); several
 * integration configs treat a 404 as "not configured yet" rather than a fault.
 */
export function isNotFoundError(error: unknown): boolean {
  return error instanceof Error && error.message.includes("status code 404");
}

export function normalizeApiError(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) {
    const jsonTail = error.message.match(/\{[\s\S]*\}$/);
    if (jsonTail) {
      try {
        const parsed = JSON.parse(jsonTail[0]) as { description?: string };
        if (parsed.description?.trim()) {
          return parsed.description.trim();
        }
      } catch {
        // Not JSON — fall through to the translated fallback.
      }
    }
  }
  return fallback;
}
