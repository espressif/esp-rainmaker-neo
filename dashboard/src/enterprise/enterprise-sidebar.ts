/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from 'i18next'
import type {
  SidebarGroupConfig,
  SidebarItemConfig,
} from '@espressif/dashboard-ui-components/components'
import type { SidebarNavEntry } from '@espressif/dashboard-ui-components/layouts'
import { isSidebarGroup } from '@espressif/dashboard-ui-components/layouts'
import { lockedCatalogEntries } from './catalog'
import type { EnterpriseCatalogEntry } from './catalog'
import {
  EnterpriseCrownIcon,
  EnterpriseGroupIcon,
  EnterpriseWarningIcon,
  lockIconLabelled,
  resolveEnterpriseIcon,
} from './enterprise-icons'
import { enterpriseNamespace } from './enterprise.types'
import { BROKEN_FEATURE_STATUSES } from './enterprise.store'
import type { EnterpriseFeature, EnterpriseStatus } from './enterprise.store'
import type { SidebarContribution } from './loader/manifest.schema'

function toSidebarItems(
  contribution: SidebarContribution,
  featureId: string,
  isBroken: boolean,
  t: TFunction,
): SidebarItemConfig[] {
  const ns = enterpriseNamespace(featureId)
  // A feature that could not run keeps its item, so the admin can reach the page that says why.
  const badge = isBroken
    ? { icon: EnterpriseWarningIcon, tooltip: t('enterprise.needsAttention', 'This enterprise feature needs attention') }
    : { icon: EnterpriseCrownIcon, tooltip: t('enterprise.badge', 'Enterprise feature') }
  return contribution.items.map((item) => ({
    id: item.id,
    label: t(`${ns}:${item.labelKey}`, item.labelFallback),
    icon: resolveEnterpriseIcon(item.icon),
    path: item.path,
    // Targeting the item's own path makes the badge click a no-op navigation.
    endAction: { ...badge, path: item.path },
  }))
}

const LOCKED_GROUP_ID = 'enterprise-locked'

/** One group, at the end of the sidebar, holding every locked feature: they stay together instead of being scattered among the working entries. */
function lockedSidebarGroup(locked: EnterpriseCatalogEntry[], t: TFunction): SidebarGroupConfig {
  const lockedText = t('enterprise.lockedBadge', 'Not part of this deployment')
  return {
    id: LOCKED_GROUP_ID,
    label: t('enterprise.lockedGroup', 'Enterprise'),
    icon: EnterpriseGroupIcon,
    items: locked.map((entry) => ({
      id: `${LOCKED_GROUP_ID}-${entry.id}`,
      label: t(entry.labelKey, entry.labelFallback),
      icon: resolveEnterpriseIcon(entry.icon),
      path: entry.path,
      endAction: {
        icon: lockIconLabelled(lockedText),
        path: entry.path,
        tooltip: lockedText,
      },
    })),
  }
}

/**
 * Merge enterprise entries into the static sidebar config: the contributions of loaded features and of features that should have loaded, then one group of locked catalog entries. Items are only ever *appended*, so the sidebar-redirect contract (`items[0].path` ↔ parent `redirectTo`, asserted in dev) keeps holding for the static groups.
 */
export function mergeEnterpriseSidebar(
  entries: SidebarNavEntry[],
  features: Record<string, EnterpriseFeature>,
  status: EnterpriseStatus,
  t: TFunction,
): SidebarNavEntry[] {
  const contributions = Object.values(features)
    .filter((f) => f.status === 'loaded' || BROKEN_FEATURE_STATUSES.includes(f.status))
    .flatMap((f) =>
      (f.entry.contributes.sidebar ?? []).map((c) => ({
        c,
        featureId: f.entry.id,
        isBroken: f.status !== 'loaded',
      })),
    )
  const locked = lockedCatalogEntries(status, features)
  if (contributions.length === 0 && locked.length === 0) {
    return entries
  }

  const merged = entries.map((entry) =>
    isSidebarGroup(entry) ? { ...entry, items: [...entry.items] } : entry,
  )

  for (const { c, featureId, isBroken } of contributions) {
    const items = toSidebarItems(c, featureId, isBroken, t)
    const existing = c.group.merge
      ? merged.find((e): e is SidebarGroupConfig => isSidebarGroup(e) && e.id === c.group.id)
      : undefined
    if (existing) {
      existing.items.push(...items)
    } else if (c.group.labelFallback) {
      const ns = enterpriseNamespace(featureId)
      merged.push({
        id: c.group.id,
        label: c.group.labelKey
          ? t(`${ns}:${c.group.labelKey}`, c.group.labelFallback)
          : c.group.labelFallback,
        icon: resolveEnterpriseIcon(c.group.icon ?? ''),
        items,
      })
    } else if (import.meta.env.DEV) {
      console.warn(
        `[enterprise] sidebar contribution from "${featureId}" targets unknown group "${c.group.id}" and carries no group label — skipped`,
      )
    }
  }
  if (locked.length > 0) {
    merged.push(lockedSidebarGroup(locked, t))
  }
  return merged
}
