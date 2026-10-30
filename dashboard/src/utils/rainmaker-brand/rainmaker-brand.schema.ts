/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

// App Studio owns the brand schema, so only what the dashboard itself relies on is checked and every other key is kept as written.

import { z } from "zod";

/** The only `schemaVersion` values this dashboard reads and publishes. */
export const SUPPORTED_SCHEMA_VERSIONS: readonly number[] = [1];
export const BRAND_THEMES = ["light", "dark"] as const;

export type BrandTheme = (typeof BRAND_THEMES)[number];

/** The config fields the branding page reads; any other key is published untouched. */
export interface BrandConfig {
  brandName: string;
  supportedThemes: BrandTheme[];
  brandTagline?: string;
  company?: string;
  brandLogo?: Partial<Record<BrandTheme, string>>;
  ui?: {
    radius?: number;
    logoScale?: number;
    colors?: Partial<Record<BrandTheme, Record<string, string>>>;
  };
  glossary?: Record<string, string>;
  assets?: { images?: Record<string, Record<string, string>> };
  [key: string]: unknown;
}

const schemaVersion = z
  .number()
  .int()
  .refine(
    (version) => SUPPORTED_SCHEMA_VERSIONS.includes(version),
    `is not supported; this dashboard reads ${SUPPORTED_SCHEMA_VERSIONS.join(", ")}`,
  );

const supportedThemes = z
  .array(z.enum(BRAND_THEMES))
  .min(1)
  .refine((themes) => new Set(themes).size === themes.length, "must not repeat a theme");

const config = z
  .looseObject({ brandName: z.string().min(1), supportedThemes })
  .transform((value) => value as BrandConfig);

/** `brand/brand.json` inside a .rainmaker file. */
export const brandDocumentSchema = z.looseObject({ schemaVersion, config });

/** `<region>/brand/brand.json` as published. */
export const publishedBrandSchema = z.looseObject({
  schemaVersion,
  checksum: z.string().regex(/^[0-9a-f]{64}$/, "must be a lower-case sha256 hex digest"),
  publishedAt: z.number().int().min(0),
  config,
});

export type BrandDocument = z.infer<typeof brandDocumentSchema>;
/** The brand as published at `<region>/brand/brand.json`. */
export type PublishedBrand = z.infer<typeof publishedBrandSchema>;
