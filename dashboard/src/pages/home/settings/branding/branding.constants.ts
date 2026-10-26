/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { CSSProperties } from "react";
import type {
  BrandMode,
  PublishedBrand,
} from "@/utils/rainmaker-brand/rainmaker-brand";

export const BRAND_CARD_PROPS = {
  allowCollapse: false,
  color: "silver",
  variant: "outline",
  size: "lg",
} as const;

/** Colour role App Studio uses for the app's screen background. */
export const BRAND_SURFACE_ROLE = "bg";

/** Backdrop for a mode whose palette leaves the screen background unset. */
export const DEFAULT_BRAND_SURFACE: Record<BrandMode, string> = {
  light: "#FFFFFF",
  dark: "#121212",
};

export const BRAND_MODES: BrandMode[] = ["light", "dark"];

export const DEFAULT_LOGO_SCALE = 1;

export const BRAND_MODES_BY_APPEARANCE: Record<
  PublishedBrand["appearance"],
  BrandMode[]
> = {
  light: ["light"],
  dark: ["dark"],
  both: BRAND_MODES,
};

export const BRAND_MODE_LABELS: Record<
  BrandMode,
  { i18nKey: string; fallback: string }
> = {
  light: { i18nKey: "branding:modes.light", fallback: "Light mode" },
  dark: { i18nKey: "branding:modes.dark", fallback: "Dark mode" },
};

/** Checkerboard behind a swatch, so the alpha of an rgba() colour shows. */
export const TRANSPARENCY_GRID_STYLE: CSSProperties = {
  background:
    "repeating-conic-gradient(#d4d4d8 0% 25%, #ffffff 0% 50%) 50% / 12px 12px",
};
