/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import catalogEntries from './catalog.json'
import { BROKEN_FEATURE_STATUSES } from './enterprise.store'
import type { EnterpriseFeature, EnterpriseStatus } from './enterprise.store'

/**
 * An enterprise feature a deployment without it is told about: one locked sidebar item and one page saying what it does. Display data only.
 */
export interface EnterpriseCatalogEntry {
  /** Same id as the feature's plugin. */
  id: string
  labelKey: string
  labelFallback: string
  descriptionKey: string
  descriptionFallback: string
  /** Icon name in the enterprise icon registry. */
  icon: string
  /** The page the plugin serves when the feature is present, under `/home/ent/`; the locked page takes its place otherwise. */
  path: string
}

/**
 * The enterprise features shown as locked where they are absent, kept in `catalog.json` so the enterprise plugin build can read it without parsing TypeScript; that build fails if an entry has no plugin serving its path. Keep it short and limited to clear extras with a page of their own: a feature a community user would expect as basic, or one that lives inside an existing screen, must never be listed, because a lock there reads as a broken free edition.
 */
export const ENTERPRISE_CATALOG: readonly EnterpriseCatalogEntry[] = catalogEntries

/**
 * Catalog entries to show as locked: those this deployment has no working or broken copy of. Empty until the deployment's own features have settled, so a locked item never flashes before the real one, and empty when loading failed outright, where the page-level alert already speaks.
 */
export function lockedCatalogEntries(
  status: EnterpriseStatus,
  features: Record<string, EnterpriseFeature>,
): EnterpriseCatalogEntry[] {
  if (status !== 'community' && status !== 'ready') {
    return []
  }
  return ENTERPRISE_CATALOG.filter((entry) => {
    const feature = features[entry.id]
    return !feature || !(feature.status === 'loaded' || BROKEN_FEATURE_STATUSES.includes(feature.status))
  })
}

export function findLockedCatalogEntry(
  status: EnterpriseStatus,
  features: Record<string, EnterpriseFeature>,
  pathname: string,
): EnterpriseCatalogEntry | undefined {
  return lockedCatalogEntries(status, features).find((entry) => entry.path === pathname)
}
