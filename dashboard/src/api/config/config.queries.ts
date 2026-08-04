/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useQuery } from '@tanstack/react-query'
import { useConfigStore } from '@/stores/config.store'
import { fetchRuntimeConfig } from './config.api'
import type { RuntimeConfig } from '@/lib/config'

/**
 * Query key factory for the runtime-config domain.
 */
export const configKeys = {
  all: ['config'] as const,
  runtime: () => [...configKeys.all, 'runtime'] as const,
}

/**
 * Fetch fresh config, then persist it into the config store (the source of truth
 * read by the getters in `@/lib/config`). Runs inside the queryFn so the store
 * stays in sync on every successful fetch, including background refreshes.
 */
async function fetchAndPersistRuntimeConfig(): Promise<RuntimeConfig> {
  const config = await fetchRuntimeConfig()
  useConfigStore.getState().setConfig(config)
  return config
}

/**
 * Query options factory for the runtime-config domain.
 * Cached-first display is driven by the persisted `useConfigStore` (see
 * AppBootstrap), so this query always refetches on mount to refresh the cache in
 * the background — we intentionally do not seed `initialData` (which, combined
 * with a long staleTime, would suppress that refresh).
 */
export const configQueries = {
  runtime: () => ({
    queryKey: configKeys.runtime(),
    queryFn: fetchAndPersistRuntimeConfig,
    staleTime: 1000 * 60 * 60, // 1 hour — deployment config is near-static
    retry: 2,
  }),
}

/**
 * Load the runtime config. Drives the app bootstrap gate (see AppBootstrap).
 *
 * `enabled` exists for routes that bypass the gate (`skipBootstrap`): they render from
 * bundled assets alone, so fetching a config they never read is pure waste.
 */
export function useRuntimeConfigQuery(options?: { enabled?: boolean }) {
  return useQuery<RuntimeConfig, Error>({
    ...configQueries.runtime(),
    enabled: options?.enabled ?? true,
  })
}
