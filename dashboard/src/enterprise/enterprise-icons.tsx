/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Crown, Lock, Network, Puzzle, ServerCog, SlidersHorizontal, TriangleAlert } from 'lucide-react'
import type { SidebarIcon } from '@/config/sidebar/sidebar-icons.config'

/**
 * Name→component registry for icons referenced by enterprise manifests. Manifests carry icon
 * *names* so JSON stays serializable and the host's icon tree-shaking stays intact; a name is
 * added here the first time a plugin needs it.
 */
const enterpriseIcons: Record<string, SidebarIcon> = {
  'network': Network,
  'sliders-horizontal': SlidersHorizontal,
  'server-cog': ServerCog,
}

export function resolveEnterpriseIcon(name: string): SidebarIcon {
  return enterpriseIcons[name] ?? Puzzle
}

/** Golden crown marking enterprise-contributed sidebar items. Inline color: the host stylesheet ships no gold utility class, and the badge must not depend on one. */
export function EnterpriseCrownIcon({ className }: { className?: string }) {
  return <Crown className={className} style={{ color: '#9c7c00' }} aria-hidden />
}

/** Marks a sidebar item whose enterprise feature belongs to this deployment but could not run. Inline color for the same reason as the crown. */
export function EnterpriseWarningIcon({ className }: { className?: string }) {
  return <TriangleAlert className={className} style={{ color: '#b45309' }} aria-hidden />
}

const lockIcons = new Map<string, SidebarIcon>()

/**
 * Lock badge for a locked sidebar item, named for screen readers: the sidebar renders the badge as a link around this icon, and the badge is the only thing marking the item as locked. Cached per label so the component identity stays stable across renders. Deliberately uncoloured: it should read as information, not as a call to action.
 */
export function lockIconLabelled(label: string): SidebarIcon {
  const cached = lockIcons.get(label)
  if (cached) {
    return cached
  }
  function EnterpriseLockIcon({ className }: { className?: string }) {
    return <Lock className={className} role="img" aria-label={label} />
  }
  lockIcons.set(label, EnterpriseLockIcon)
  return EnterpriseLockIcon
}

/** Icon of the sidebar group that collects the locked items. */
export const EnterpriseGroupIcon: SidebarIcon = Crown
