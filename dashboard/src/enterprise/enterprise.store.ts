/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { create } from 'zustand'
import { matchPathTemplate } from '@/lib/navigation/match-path-template'
import type { EnterpriseComponent } from './enterprise.types'
import type { ExtensionContribution, ManifestFeature, RouteContribution } from './loader/manifest.schema'

/**
 * Runtime state of the enterprise plugin mechanism. Deliberately not persisted:
 * bundles are re-fetched and re-verified every session, so stale state cannot
 * outlive the code it describes.
 */

export type EnterpriseStatus = 'idle' | 'loading' | 'ready' | 'community' | 'error'

export type FeatureStatus = 'loaded' | 'unavailable' | 'incompatible'

export interface EnterpriseFeature {
  entry: ManifestFeature
  status: FeatureStatus
  components: Record<string, EnterpriseComponent>
}

interface EnterpriseState {
  status: EnterpriseStatus
  features: Record<string, EnterpriseFeature>
  setStatus: (status: EnterpriseStatus) => void
  upsertFeature: (feature: EnterpriseFeature) => void
}

export const useEnterpriseStore = create<EnterpriseState>()((set) => ({
  status: 'idle',
  features: {},
  setStatus: (status) => set({ status }),
  upsertFeature: (feature) =>
    set((s) => ({ features: { ...s.features, [feature.entry.id]: feature } })),
}))

export interface EnterpriseRouteMatch {
  feature: EnterpriseFeature
  route: RouteContribution
  params: Record<string, string>
}

/** Match a pathname against every known feature's manifest route templates. */
export function findEnterpriseRoute(
  features: Record<string, EnterpriseFeature>,
  pathname: string,
): EnterpriseRouteMatch | null {
  for (const feature of Object.values(features)) {
    for (const route of feature.entry.contributes.routes ?? []) {
      const params = matchPathTemplate(route.path, pathname)
      if (params) {
        return { feature, route, params }
      }
    }
  }
  return null
}

export interface ExtensionMatch {
  feature: EnterpriseFeature
  extension: ExtensionContribution
}

/**
 * All loaded features' contributions targeting one slot. Sorted by `order`, then
 * feature id, then declaration order — feature insertion order is load-completion
 * order and therefore not stable enough to render by.
 */
export function findExtensionContributions(
  features: Record<string, EnterpriseFeature>,
  slotId: string,
): ExtensionMatch[] {
  const matches: (ExtensionMatch & { index: number })[] = []
  for (const feature of Object.values(features)) {
    if (feature.status !== 'loaded') {
      continue
    }
    ;(feature.entry.contributes.extensions ?? []).forEach((extension, index) => {
      if (extension.slotId === slotId) {
        matches.push({ feature, extension, index })
      }
    })
  }
  return matches
    .sort(
      (a, b) =>
        (a.extension.order ?? 0) - (b.extension.order ?? 0) ||
        a.feature.entry.id.localeCompare(b.feature.entry.id) ||
        a.index - b.index,
    )
    .map(({ feature, extension }) => ({ feature, extension }))
}
