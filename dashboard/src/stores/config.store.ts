/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { appStorageKey } from '@/lib/app-config'
import type { RuntimeConfig } from '@/lib/config'

/**
 * Runtime configuration cache.
 *
 * Holds the flattened `RuntimeConfig` fetched from the published client-outputs
 * document at startup. Persisted to localStorage so returning users boot instantly
 * from cache while a fresh copy is refetched in the background (see AppBootstrap).
 * This store is the single source of truth read by the getters in `@/lib/config`.
 *
 * The values here (API URLs, region, bucket/role names, Cognito ids) are public
 * deployment metadata served openly via config.json — not secrets.
 */
interface ConfigState {
  config: RuntimeConfig | null
  setConfig: (config: RuntimeConfig) => void
  clearConfig: () => void
}

export const useConfigStore = create<ConfigState>()(
  persist(
    (set) => ({
      config: null,
      setConfig: (config) => set({ config }),
      clearConfig: () => set({ config: null }),
    }),
    {
      name: appStorageKey('config-storage'),
    }
  )
)
