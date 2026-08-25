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
import { EnterpriseCrownIcon, resolveEnterpriseIcon } from './enterprise-icons'
import { enterpriseNamespace } from './enterprise.types'
import type { EnterpriseFeature } from './enterprise.store'
import type { SidebarContribution } from './loader/manifest.schema'

function toSidebarItems(
  contribution: SidebarContribution,
  featureId: string,
  t: TFunction,
): SidebarItemConfig[] {
  const ns = enterpriseNamespace(featureId)
  return contribution.items.map((item) => ({
    id: item.id,
    label: t(`${ns}:${item.labelKey}`, item.labelFallback),
    icon: resolveEnterpriseIcon(item.icon),
    path: item.path,
    // Every enterprise item carries the golden crown; targeting the item's own path makes the badge click a no-op navigation.
    endAction: {
      icon: EnterpriseCrownIcon,
      path: item.path,
      tooltip: t('enterprise.badge', 'Enterprise feature'),
    },
  }))
}

/**
 * Merge loaded features' sidebar contributions into the static config. Items are only ever
 * *appended*, so the sidebar-redirect contract (`items[0].path` ↔ parent `redirectTo`,
 * asserted in dev) keeps holding for the static groups.
 */
export function mergeEnterpriseSidebar(
  entries: SidebarNavEntry[],
  features: Record<string, EnterpriseFeature>,
  t: TFunction,
): SidebarNavEntry[] {
  const contributions = Object.values(features)
    .filter((f) => f.status === 'loaded')
    .flatMap((f) =>
      (f.entry.contributes.sidebar ?? []).map((c) => ({ c, featureId: f.entry.id })),
    )
  if (contributions.length === 0) {
    return entries
  }

  const merged = entries.map((entry) =>
    isSidebarGroup(entry) ? { ...entry, items: [...entry.items] } : entry,
  )

  for (const { c, featureId } of contributions) {
    const items = toSidebarItems(c, featureId, t)
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
  return merged
}
