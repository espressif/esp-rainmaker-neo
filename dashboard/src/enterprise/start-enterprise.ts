/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { getEnterprisePluginsBucket } from '@/lib/config'
import { useEnterpriseStore } from './enterprise.store'
import { clearCachedEnterpriseTheme } from './theme-cache'

function hasDevPlugin(): boolean {
  return import.meta.env.DEV && new URLSearchParams(window.location.search).has('enterpriseDevUrl')
}

/**
 * Decide community vs enterprise from the deployment's own config, before any enterprise code is fetched: a deployment that names no plugins bucket never downloads the loader chunk. Call once credentials exist, since the loader reads the bucket with them, and again whenever the config's plugins bucket changes: the first render uses a cached config that the background refresh may correct.
 */
export function startEnterprise(): void {
  const store = useEnterpriseStore.getState()
  if (!getEnterprisePluginsBucket() && !hasDevPlugin()) {
    // A deployment whose plugins were removed must stop re-applying their cached theme.
    clearCachedEnterpriseTheme()
    store.clearFeatures()
    store.setStatus('community')
    return
  }
  import('./loader')
    .then((loader) => loader.ensureEnterpriseLoaded())
    .catch((error: unknown) => {
      console.error('[enterprise] loader chunk failed to load:', error)
      store.setStatus('error')
    })
}
