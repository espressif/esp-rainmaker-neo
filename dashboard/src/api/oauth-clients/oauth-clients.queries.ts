/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useQuery } from '@tanstack/react-query'
import { oauthClientsApi } from './oauth-clients.api'
import type { ListOAuthClientsResponse } from './oauth-clients.types'

/**
 * Query key factory for the admin OAuth clients domain
 */
export const oauthClientsKeys = {
  all: ['oauth-clients'] as const,
  list: (getSecret: boolean) => [...oauthClientsKeys.all, 'list', { getSecret }] as const,
}

/**
 * Query options factory for the admin OAuth clients domain
 */
export const oauthClientsQueries = {
  list: (getSecret: boolean) => ({
    queryKey: oauthClientsKeys.list(getSecret),
    queryFn: () => oauthClientsApi.list({ getSecret }),
    staleTime: 1000 * 60 * 5, // 5 minutes
    retry: 1,
  }),
}

/**
 * Hook for listing the registered OAuth clients.
 */
export function useOAuthClients(options?: { getSecret?: boolean; enabled?: boolean }) {
  return useQuery<ListOAuthClientsResponse, Error>({
    ...oauthClientsQueries.list(options?.getSecret ?? false),
    enabled: options?.enabled ?? true,
  })
}

/**
 * Hook for one registered client, secret included. The listing is superadmin-only, so callers get
 * `undefined` — never a thrown error — when the request is refused or the client is gone; these
 * are values to display when available, not something to fail a page over.
 */
export function useOAuthClient(clientId: string, options?: { enabled?: boolean }) {
  const query = useOAuthClients({
    getSecret: true,
    enabled: (options?.enabled ?? true) && clientId.length > 0,
  })

  const client = query.data?.clients?.find(entry => entry.client_id === clientId)

  return { client, isLoading: query.isLoading, isError: query.isError }
}
