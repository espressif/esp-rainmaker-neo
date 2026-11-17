/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import enCommon from '@/i18n/locales/en/common.json'
import zhCommon from '@/i18n/locales/zh/common.json'
import { ENTERPRISE_CATALOG, findLockedCatalogEntry, lockedCatalogEntries } from './catalog'
import type { EnterpriseFeature, FeatureStatus } from './enterprise.store'
import { ENTERPRISE_ROUTE_PREFIX } from './enterprise.types'

const first = ENTERPRISE_CATALOG[0]

/** ('common:enterprise.lockedGroup', enCommon) -> 'Enterprise'; undefined when any segment is missing. */
function lookupCommonKey(qualifiedKey: string, locale: unknown): unknown {
  const [, path] = qualifiedKey.split(':')
  return path.split('.').reduce<unknown>(
    (node, segment) => (node as Record<string, unknown> | undefined)?.[segment],
    locale,
  )
}

describe('ENTERPRISE_CATALOG', () => {
  it('places every entry under the enterprise catch-all route, with a unique id', () => {
    for (const entry of ENTERPRISE_CATALOG) {
      expect(entry.path.startsWith(ENTERPRISE_ROUTE_PREFIX)).toBe(true)
    }
    expect(new Set(ENTERPRISE_CATALOG.map((e) => e.id)).size).toBe(ENTERPRISE_CATALOG.length)
  })

  // check:i18n scans only .ts/.tsx sources, so the keys in catalog.json are checked here instead.
  it('names common keys that exist in both locales', () => {
    for (const entry of ENTERPRISE_CATALOG) {
      for (const key of [entry.labelKey, entry.descriptionKey]) {
        expect(key.startsWith('common:')).toBe(true)
        expect(typeof lookupCommonKey(key, enCommon), `${key} in en`).toBe('string')
        expect(typeof lookupCommonKey(key, zhCommon), `${key} in zh`).toBe('string')
      }
    }
  })
})

function featureWith(status: FeatureStatus): Record<string, EnterpriseFeature> {
  const entry = { id: first.id, version: '1.0.0', sdk: '^1.0', bundle: 'b.js', integrity: 'x', contributes: {} }
  return { [first.id]: { entry, status, components: {} } }
}

describe('lockedCatalogEntries', () => {
  it('locks every catalog entry on a community deployment', () => {
    expect(lockedCatalogEntries('community', {})).toEqual(ENTERPRISE_CATALOG)
  })

  it('shows nothing locked until the deployment has settled, or when loading failed', () => {
    expect(lockedCatalogEntries('idle', {})).toEqual([])
    expect(lockedCatalogEntries('loading', {})).toEqual([])
    expect(lockedCatalogEntries('error', {})).toEqual([])
  })

  it('does not lock a feature the deployment has, working or broken', () => {
    for (const status of ['loaded', 'incompatible', 'version-mismatch', 'failed'] as const) {
      expect(lockedCatalogEntries('ready', featureWith(status)).map((e) => e.id)).not.toContain(first.id)
    }
  })

  it('locks a feature whose backend is not deployed', () => {
    expect(lockedCatalogEntries('ready', featureWith('not-deployed')).map((e) => e.id)).toContain(first.id)
  })
})

describe('findLockedCatalogEntry', () => {
  it('finds the locked entry for its own path only', () => {
    expect(findLockedCatalogEntry('community', {}, first.path)?.id).toBe(first.id)
    expect(findLockedCatalogEntry('community', {}, '/home/ent/unknown')).toBeUndefined()
    expect(findLockedCatalogEntry('ready', featureWith('loaded'), first.path)).toBeUndefined()
  })
})
