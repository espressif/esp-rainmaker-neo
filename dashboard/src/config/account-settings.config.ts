/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react";
import { KeyRound, SlidersHorizontal, UserRound } from "lucide-react";

type AccountSettingsTabIcon = ComponentType<{ className?: string }>;

/** Path segment of the section, relative to the authenticated `/home` route tree. */
export const ACCOUNT_SETTINGS_ROUTE_SEGMENT = "/account-settings";

/** Root of the account settings section. Every tab path is derived from this. */
export const ACCOUNT_SETTINGS_BASE_PATH = `/home${ACCOUNT_SETTINGS_ROUTE_SEGMENT}`;

export type AccountSettingsTabId = "profile" | "preferences" | "password";

export type AccountSettingsTab = {
  /** Stable identifier, also the URL segment under {@link ACCOUNT_SETTINGS_BASE_PATH}. */
  id: AccountSettingsTabId;
  /** i18n key under the `account-settings` namespace. */
  labelKey: string;
  /** English fallback used until the key is translated. */
  fallback: string;
  /** Identity icon shown in the nav rail and the tab heading. */
  icon: AccountSettingsTabIcon;
  /** Absolute route to the tab page. */
  path: string;
};

/**
 * Single source of truth for the account settings tabs.
 *
 * Drives the desktop nav rail, the small-screen tab picker, each tab page's heading,
 * and the route subtree in [app-routes.config.ts](./app-routes.config.ts) — which
 * derives its subroutes from this list rather than repeating the paths. Add, remove or
 * reorder a tab here and every surface updates; do not hardcode tab labels or paths
 * elsewhere.
 */
export const ACCOUNT_SETTINGS_TABS = [
  {
    id: "profile",
    labelKey: "account-settings:tabs.profile",
    fallback: "Profile",
    icon: UserRound,
    path: `${ACCOUNT_SETTINGS_BASE_PATH}/profile`,
  },
  {
    id: "preferences",
    labelKey: "account-settings:tabs.preferences",
    fallback: "App preferences",
    icon: SlidersHorizontal,
    path: `${ACCOUNT_SETTINGS_BASE_PATH}/preferences`,
  },
  {
    id: "password",
    labelKey: "account-settings:tabs.password",
    fallback: "Change password",
    icon: KeyRound,
    path: `${ACCOUNT_SETTINGS_BASE_PATH}/password`,
  },
] as const satisfies readonly AccountSettingsTab[];

export const ACCOUNT_SETTINGS_TABS_BY_ID = Object.fromEntries(
  ACCOUNT_SETTINGS_TABS.map((tab) => [tab.id, tab]),
) as Record<AccountSettingsTabId, (typeof ACCOUNT_SETTINGS_TABS)[number]>;

/** Tab the redirect from {@link ACCOUNT_SETTINGS_BASE_PATH} lands on. */
export const DEFAULT_ACCOUNT_SETTINGS_TAB = ACCOUNT_SETTINGS_TABS[0];

function isAccountSettingsTabId(value: string): value is AccountSettingsTabId {
  return value in ACCOUNT_SETTINGS_TABS_BY_ID;
}

/**
 * Resolves the active tab from a pathname, matching only the segment directly under
 * the base path so future per-tab subroutes still resolve to their parent tab.
 *
 * Returns `null` for anything outside the section (or an unknown tab), letting callers
 * render an unselected nav instead of guessing.
 */
export function getActiveAccountSettingsTabId(
  pathname: string,
): AccountSettingsTabId | null {
  if (!pathname.startsWith(ACCOUNT_SETTINGS_BASE_PATH)) {
    return null;
  }

  const [segment] = pathname
    .slice(ACCOUNT_SETTINGS_BASE_PATH.length)
    .split("/")
    .filter(Boolean);

  if (!segment || !isAccountSettingsTabId(segment)) {
    return null;
  }

  return segment;
}
