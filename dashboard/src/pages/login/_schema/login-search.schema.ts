/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { z } from "zod";
import { isInternalPath } from "@/lib/navigation/internal-path";

// Permissive: anything other than the exact success marker is treated as
// absent, so a hand-edited query string degrades to a plain sign-in form.
const loginSearchSchema = z.object({
  reset: z.preprocess(
    (value) => (value === "success" ? value : undefined),
    z.literal("success").optional(),
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
