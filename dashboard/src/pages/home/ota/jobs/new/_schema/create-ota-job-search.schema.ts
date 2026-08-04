/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { z } from "zod";

// Permissive: trims whitespace, treats empty as absent, and never throws so a
// malformed deep link degrades to a normal empty form instead of erroring.
const optionalTrimmedString = z.preprocess((value) => {
  if (typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}, z.string().optional());

const createOtaJobSearchSchema = z.object({
  firmware_key: optionalTrimmedString,
});

export type CreateOtaJobSearch = z.infer<typeof createOtaJobSearchSchema>;

export function parseCreateOtaJobSearch(search: unknown): CreateOtaJobSearch {
  return createOtaJobSearchSchema.parse(search);
}
