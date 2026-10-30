/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { CSSProperties } from "react";
import type { BrandTheme } from "@/utils/rainmaker-brand/rainmaker-brand";

export const BRAND_IMAGE_TILE_CLASS =
  "flex items-center justify-center overflow-hidden border border-border";
export const BRAND_ITEM_CLASS = "rounded-xl border border-border";

export const BRAND_CARD_PROPS = {
  allowCollapse: false,
  color: "silver",
  variant: "outline",
  size: "lg",
} as const;

/** Colour role App Studio uses for the app's screen background. */
export const BRAND_SURFACE_ROLE = "bg";

/** Backdrop for a theme whose palette leaves the screen background unset. */
export const DEFAULT_BRAND_SURFACE: Record<BrandTheme, string> = {
  light: "#FFFFFF",
  dark: "#121212",
};

export const BRAND_MODES: BrandTheme[] = ["light", "dark"];

export const DEFAULT_LOGO_SCALE = 1;

export const BRAND_MODE_LABELS: Record<
  BrandTheme,
  { i18nKey: string; fallback: string }
> = {
  light: { i18nKey: "branding:modes.light", fallback: "Light mode" },
  dark: { i18nKey: "branding:modes.dark", fallback: "Dark mode" },
};

/** Headings for the image groups App Studio writes; a group this dashboard does not know shows its own name. */
export const BRAND_ICON_GROUP_LABELS: Partial<
  Record<string, { i18nKey: string; fallback: string }>
> = {
  devices: { i18nKey: "branding:icons.groups.devices", fallback: "Devices" },
  agents: { i18nKey: "branding:icons.groups.agents", fallback: "AI agents" },
  illustrations: {
    i18nKey: "branding:icons.groups.illustrations",
    fallback: "Illustrations",
  },
};

/** Checkerboard behind a swatch, so the alpha of an rgba() colour shows. */
export const TRANSPARENCY_GRID_STYLE: CSSProperties = {
  background:
    "repeating-conic-gradient(#d4d4d8 0% 25%, #ffffff 0% 50%) 50% / 12px 12px",
};
