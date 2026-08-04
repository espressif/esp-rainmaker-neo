/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { z } from "zod";

// Permissive: a blank or whitespace-only value is treated as absent, and a
// malformed address fails the parse. Both land on the same "no email" branch,
// so a hand-edited deep link degrades to a redirect instead of an error screen.
const optionalEmail = z.preprocess((value) => {
  if (typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}, z.string().email().optional());

const setPasswordSearchSchema = z.object({
  email: optionalEmail,
  // Set only when a code was just mailed, so the "check your inbox" notice does
  // not show for admins who arrived saying they already hold a code.
  // The router parses search values with JSON.parse, so `sent=true` arrives as
  // a boolean; the string forms cover a hand-typed link.
  sent: z.preprocess(
    (value) => value === true || value === "true" || value === "1",
    z.boolean(),
  ),
});

export type SetPasswordSearch = z.infer<typeof setPasswordSearchSchema>;

export function parseSetPasswordSearch(search: unknown): SetPasswordSearch {
  const result = setPasswordSearchSchema.safeParse(search);
  return result.success ? result.data : { sent: false };
}
