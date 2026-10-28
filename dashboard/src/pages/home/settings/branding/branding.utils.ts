/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { S3ServiceException } from "@aws-sdk/client-s3";
import { normalizeApiError } from "@/lib/normalize-api-error";
import {
  stringFields,
  type BrandConfig,
  type BrandTheme,
  type PublishedBrand,
} from "@/utils/rainmaker-brand/rainmaker-brand";
import {
  BRAND_ICON_GROUP_LABELS,
  BRAND_MODES,
  BRAND_SURFACE_ROLE,
  DEFAULT_BRAND_SURFACE,
  DEFAULT_LOGO_SCALE,
} from "./branding.constants";

const ACCESS_DENIED = "AccessDenied";

export type BrandAppearance = BrandTheme | "both";

export interface BrandIconGroup {
  group: string;
  label?: { i18nKey: string; fallback: string };
  icons: [name: string, url: string][];
}

/** Turns a failed branding read or write into a translated message; a deployment not yet redeployed with branding says so. */
export function getBrandingErrorMessage(
  error: unknown,
  t: TFunction,
  fallback: string,
): string {
  if (error instanceof S3ServiceException && error.name === ACCESS_DENIED) {
    return t(
      "errors.unavailable",
      "This deployment does not allow branding from the dashboard yet. Redeploy it and try again.",
    );
  }
  return normalizeApiError(error, fallback);
}

/** `["light", "dark"]` -> `"both"`, `["dark"]` -> `"dark"`. */
export function getBrandAppearance(themes: BrandTheme[]): BrandAppearance {
  return themes.length > 1 ? "both" : themes[0];
}

/** The screen background the app paints in a theme, which logos and icons are previewed on. */
function getBrandSurface(config: BrandConfig, theme: BrandTheme): string {
  return (
    config.ui?.colors?.[theme]?.[BRAND_SURFACE_ROLE] ??
    DEFAULT_BRAND_SURFACE[theme]
  );
}

function isText(value: unknown): value is string {
  return typeof value === "string" && value !== "";
}

/** The logo the app shows in a theme: its own, else the other theme's. */
function getBrandLogo(
  config: BrandConfig,
  theme: BrandTheme,
): string | undefined {
  const other = BRAND_MODES.find((mode) => mode !== theme);
  return [config.brandLogo?.[theme], other && config.brandLogo?.[other]].find(
    isText,
  );
}

function getIconGroups(config: BrandConfig): BrandIconGroup[] {
  return Object.entries(config.assets?.images ?? {})
    .map(([group, icons]) => ({
      group,
      label: BRAND_ICON_GROUP_LABELS[group],
      icons: Object.entries(stringFields(icons)),
    }))
    .filter(({ icons }) => icons.length > 0);
}

/** What the preview draws, with every inline style built once; publishing does not check value types, so values that are not text are skipped. */
export function getBrandPreview({ config }: PublishedBrand) {
  const themes = BRAND_MODES.filter((mode) => config.supportedThemes.includes(mode));
  return {
    logos: themes.map((mode) => ({
      mode,
      src: getBrandLogo(config, mode),
      style: { backgroundColor: getBrandSurface(config, mode) },
    })),
    iconStyle: { backgroundColor: getBrandSurface(config, themes[0]) },
    palettes: themes.flatMap((mode) => {
      const swatches = Object.entries(config.ui?.colors?.[mode] ?? {})
        .filter((entry): entry is [string, string] => isText(entry[1]))
        .map(([role, value]) => ({
          role,
          value,
          style: { backgroundColor: value },
        }));
      return swatches.length > 0 ? [{ mode, swatches }] : [];
    }),
    iconGroups: getIconGroups(config),
    words: Object.entries(config.glossary ?? {}).filter(
      (entry): entry is [string, string] => isText(entry[1]),
    ),
  };
}

export function getLogoFacts(config: BrandConfig, t: TFunction) {
  const radius = config.ui?.radius;
  return [
    {
      label: t("logo.scale", "Logo size"),
      value: `${config.ui?.logoScale ?? DEFAULT_LOGO_SCALE}×`,
    },
    {
      label: t("logo.radius", "Corner radius"),
      value:
        radius === undefined
          ? t("logo.radiusDefault", "App default")
          : `${radius} px`,
    },
  ];
}
