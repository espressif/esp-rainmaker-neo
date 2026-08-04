/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useQuery } from '@tanstack/react-query'
import { licenseApi } from './license.api'
import type { LicenseQuotaResponse } from './license.types'

/**
 * Query key factory for license domain
 */
export const licenseKeys = {
  all: ['license'] as const,
  quota: () => [...licenseKeys.all, 'quota'] as const,
}

/**
 * Query options factory for license domain
 */
export const licenseQueries = {
  quota: () => ({
    queryKey: licenseKeys.quota(),
    queryFn: licenseApi.getQuota,
    staleTime: 1000 * 60 * 5, // 5 minutes
    retry: 1,
  }),
}

/**
 * Hook for fetching the node quota (used / total) for the account.
 */
export function useGetQuota(options?: { enabled?: boolean }) {
  return useQuery<LicenseQuotaResponse, Error>({
    ...licenseQueries.quota(),
    enabled: options?.enabled ?? true,
  })
}
