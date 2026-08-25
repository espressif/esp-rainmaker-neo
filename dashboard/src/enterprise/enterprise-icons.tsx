/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Crown, Network, Puzzle, ServerCog, SlidersHorizontal } from 'lucide-react'
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
