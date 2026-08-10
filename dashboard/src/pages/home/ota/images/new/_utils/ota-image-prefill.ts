/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { EspImageInfo } from "@/utils/esp-image/esp-image";

export interface OtaImagePrefillFields {
  version: string;
  model: string;
  platform: string;
}

export type OtaImagePrefillField = keyof OtaImagePrefillFields;

/** Fields whose value came from the image, and which the form therefore locks. */
export type OtaImageLockedFields = ReadonlySet<OtaImagePrefillField>;

/** Stable empty set, so an extraction that locks nothing is referentially equal to the last one and does not re-render. */
export const NO_LOCKED_OTA_IMAGE_FIELDS: OtaImageLockedFields = new Set();

// The binary is the source of truth for every field it carries, so an extracted value is written into the form and its field is locked outright rather than compared against what the admin typed: a value that contradicts the image can no longer be entered, which is why there is no mismatch policy here. Fields the header cannot answer for — firmware name, type — stay admin-owned.
export function computeOtaImagePrefill(
  info: EspImageInfo,
): Partial<OtaImagePrefillFields> {
  const extracted: Record<OtaImagePrefillField, string | undefined> = {
    version: info.fwVersion,
    model: info.model,
    platform: info.platform,
  };

  const values: Partial<OtaImagePrefillFields> = {};
  for (const field of Object.keys(extracted) as OtaImagePrefillField[]) {
    const value = extracted[field];
    if (value !== undefined) {
      values[field] = value;
    }
  }
  return values;
}

export function lockedFieldsFor(
  values: Partial<OtaImagePrefillFields>,
): OtaImageLockedFields {
  const fields = Object.keys(values) as OtaImagePrefillField[];
  return fields.length === 0 ? NO_LOCKED_OTA_IMAGE_FIELDS : new Set(fields);
}
