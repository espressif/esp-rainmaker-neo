/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { appConfig, type LogoAsset } from "@/lib/app-config";
import { resolveAssetPath } from "@/lib/asset-resolver";

/**
 * The configured logo assets with their `src` paths resolved to bundled URLs.
 *
 * A module-level constant rather than a hook — `appConfig` and the asset glob are both
 * static. Stays `undefined` when `appConfig.logo` is unset, which is what makes every
 * layout fall back to its `appName` preset.
 */
export const brandLogoAssets = appConfig.logo && {
  full: {
    light: { ...appConfig.logo.full.light, src: resolveAssetPath(appConfig.logo.full.light.src) },
    dark: { ...appConfig.logo.full.dark, src: resolveAssetPath(appConfig.logo.full.dark.src) },
  },
  minimal: {
    light: {
      ...appConfig.logo.minimal.light,
      src: resolveAssetPath(appConfig.logo.minimal.light.src),
    },
    dark: {
      ...appConfig.logo.minimal.dark,
      src: resolveAssetPath(appConfig.logo.minimal.dark.src),
    },
  },
};

/** Width bound per slot, matching what the library's own `AppLogo` wrapper applies. */
const LOGO_SLOT_MAX_WIDTH = {
  full: "max-w-[calc(var(--espd-sidebar-width)-32px)]",
  minimal: "max-w-[calc(var(--espd-sidebar-width-collapsed)-16px)]",
  mobile: "max-w-6",
} as const;

/**
 * Renders one `WorkspaceLayout` logo slot as a plain `<img>`.
 *
 * Deliberately bypasses the library's `logoAssets` prop, which routes through `AppLogo`.
 * `AppLogo` swaps its asset inside an `AnimatePresence mode="wait"` keyed on the asset URL,
 * and that swap does not fire when the sidebar collapses or expands — the previously
 * rendered variant stays mounted, and can be left stranded at `opacity: 0`. Reproducible on
 * `appName` presets too, so it is a library defect rather than something about our config.
 *
 * Supplying `logo` / `logoCollapsed` / `logoMobile` instead keeps each slot deterministic:
 * `WorkspaceLayout` picks the slot from the sidebar state and renders our node verbatim.
 * That also means no wrapper constrains the image, hence the explicit size classes.
 *
 * `EntryLayout` has no collapsible rail, so it takes `logoAssets` directly and lets the
 * library size it — see `brandLogoAssets`.
 */
function BrandLogo({
  asset,
  slot,
}: {
  asset: LogoAsset;
  slot: keyof typeof LOGO_SLOT_MAX_WIDTH;
}) {
  const { t } = useTranslation("common");

  return (
    <img
      src={asset.src}
      alt={t("logoAlt", "Logo")}
      width={asset.width}
      height={asset.height}
      className={`h-auto w-auto max-h-[calc(var(--espd-header-height)-16px)] object-contain ${LOGO_SLOT_MAX_WIDTH[slot]}`}
    />
  );
}

/**
 * True when `appConfig.projectName` names a library logo preset.
 *
 * The single place the preset-vs-configured decision is made. A fork omits `projectName`
 * — there is no valid `AppName` for a non-Espressif product — and every layout then
 * renders the configured assets instead.
 */
const hasLogoPreset = appConfig.projectName !== undefined;

/**
 * The three `WorkspaceLayout` logo slots, or nothing at all so that an unset `appConfig.logo`
 * still falls through to the `appName` preset — passing a node that renders `null` would
 * suppress the preset instead of deferring to it.
 *
 * Unconditional: for shells that show the deployment's own mark even on a first-party build.
 */
export function configuredLogoSlots(darkMode: boolean) {
  if (!brandLogoAssets) {
    return {};
  }

  const theme = darkMode ? "dark" : "light";

  return {
    logo: <BrandLogo asset={brandLogoAssets.full[theme]} slot="full" />,
    logoCollapsed: <BrandLogo asset={brandLogoAssets.minimal[theme]} slot="minimal" />,
    logoMobile: <BrandLogo asset={brandLogoAssets.minimal[theme]} slot="mobile" />,
  };
}

/**
 * Same slots, but only when no preset applies. Spread into layouts that should keep
 * showing the `projectName` preset on a first-party build.
 */
export function presetFallbackLogoSlots(darkMode: boolean) {
  return hasLogoPreset ? {} : configuredLogoSlots(darkMode);
}

/**
 * Asset config for `EntryLayout`'s `logoAssets` and bare `AppLogo`, which take the nested
 * config rather than per-slot nodes. `undefined` while a preset applies, so the preset wins.
 *
 * Neither renders a collapsible rail, so the `AppLogo` swap defect that forces nodes on
 * `WorkspaceLayout` does not apply here.
 */
export const presetFallbackLogoAssets = hasLogoPreset ? undefined : brandLogoAssets;
