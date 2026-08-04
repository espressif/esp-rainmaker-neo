/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useQuery } from '@tanstack/react-query'
import { userApi } from './user.api'
import type { UserCredsResponse } from './user.types'

/**
 * Query key factory for user domain
 */
export const userKeys = {
  all: ['user'] as const,
  creds: () => [...userKeys.all, 'creds'] as const,
}

/**
 * Query options factory for user domain
 */
export const userQueries = {
  creds: () => ({
    queryKey: userKeys.creds(),
    queryFn: userApi.getCreds,
    staleTime: 1000 * 60 * 30, // 30 minutes
    retry: 1,
  }),
}

/**
 * Hook for fetching temporary AWS credentials
 */
export function useGetUserCreds(options?: { enabled?: boolean }) {
  return useQuery<UserCredsResponse, Error>({
    ...userQueries.creds(),
    enabled: options?.enabled ?? true,
  })
}
