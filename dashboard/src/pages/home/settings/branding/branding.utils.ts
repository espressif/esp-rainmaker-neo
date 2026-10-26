/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { S3ServiceException } from "@aws-sdk/client-s3";
import { normalizeApiError } from "@/lib/normalize-api-error";
import {
  WORDMARK_HIDDEN,
  type BrandConfig,
  type BrandMode,
  type PublishedBrand,
} from "@/utils/rainmaker-brand/rainmaker-brand";
import {
  BRAND_MODES,
  BRAND_MODES_BY_APPEARANCE,
  BRAND_SURFACE_ROLE,
  DEFAULT_BRAND_SURFACE,
  DEFAULT_LOGO_SCALE,
} from "./branding.constants";

const ACCESS_DENIED = "AccessDenied";

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

/** The screen background the app paints in a mode, which logos and icons are previewed on. */
function getBrandSurface(config: BrandConfig, mode: BrandMode): string {
  return (
    config.colors?.[mode]?.[BRAND_SURFACE_ROLE] ?? DEFAULT_BRAND_SURFACE[mode]
  );
}

/** The logo the app shows in a mode: `logoUrlDark` in dark mode when the brand has one. */
function getBrandLogo(
  config: BrandConfig,
  mode: BrandMode,
): string | undefined {
  if (mode === "dark") {
    return config.logoUrlDark ?? config.logoUrl;
  }
  return config.logoUrl;
}

/** What the preview draws, with every inline style built once. */
export function getBrandPreview({ appearance, config }: PublishedBrand) {
  return {
    logos: BRAND_MODES_BY_APPEARANCE[appearance].map((mode) => ({
      mode,
      src: getBrandLogo(config, mode),
      style: { backgroundColor: getBrandSurface(config, mode) },
    })),
    iconStyle: { backgroundColor: getBrandSurface(config, config.mode) },
    palettes: BRAND_MODES.flatMap((mode) => {
      const swatches = Object.entries(config.colors?.[mode] ?? {}).map(
        ([role, value]) => ({ role, value, style: { backgroundColor: value } }),
      );
      return swatches.length > 0 ? [{ mode, swatches }] : [];
    }),
    icons: Object.entries(config.assets ?? {}),
    words: Object.entries(config.glossary ?? {}),
  };
}

function getWordmarkLabel(wordmark: string | undefined, t: TFunction): string {
  if (wordmark === WORDMARK_HIDDEN) {
    return t("logo.wordmarkHidden", "Hidden");
  }
  if (wordmark) {
    return t("logo.wordmarkImage", "Custom image");
  }
  return t("logo.wordmarkDefault", "Deployment name");
}

export function getLogoFacts(config: BrandConfig, t: TFunction) {
  return [
    {
      label: t("logo.wordmark", "Wordmark"),
      value: getWordmarkLabel(config.wordmark, t),
    },
    {
      label: t("logo.scale", "Logo size"),
      value: `${config.logoScale ?? DEFAULT_LOGO_SCALE}×`,
    },
    {
      label: t("logo.radius", "Corner radius"),
      value:
        config.radius === undefined
          ? t("logo.radiusDefault", "App default")
          : `${config.radius} px`,
    },
  ];
}
