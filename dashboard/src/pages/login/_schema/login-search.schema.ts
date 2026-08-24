/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { z } from "zod";
import { isInternalPath } from "@/lib/navigation/internal-path";

// Permissive: anything other than one of the two known outcomes is treated as
// absent, so a hand-edited query string degrades to a plain sign-in form.
// "success" is a password change; "set" is a first password adopted from
// account settings by an admin who previously had none — the two read
// differently on the login page, so they stay distinguishable here.
const loginSearchSchema = z.object({
  reset: z.preprocess(
    (value) => (value === "success" || value === "set" ? value : undefined),
    z.enum(["success", "set"]).optional(),
  ),
  // Same treatment for the post-login destination, with the open-redirect guard as
  // the filter: an off-site or malformed target degrades to the default `/home`.
  redirect: z.preprocess(
    (value) => (isInternalPath(value) ? value : undefined),
    z.string().optional(),
  ),
});

export type LoginSearch = z.infer<typeof loginSearchSchema>;

export function parseLoginSearch(search: unknown): LoginSearch {
  const result = loginSearchSchema.safeParse(search);
  return result.success ? result.data : {};
}
