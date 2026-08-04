/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { getApiGatewayUrl } from '@/lib/config'
import { sigv4Request } from '@/api/sigv4-client'
import type { LicenseQuotaResponse } from './license.types'

const ENDPOINTS = {
  quota: '/v1/admin/licenses/latest/quota',
} as const

function buildUrl(path: string): string {
  const baseUrl = getApiGatewayUrl()
  if (!baseUrl) {
    throw new Error('API Gateway URL is not configured')
  }
  return `${baseUrl.replace(/\/$/, '')}${path}`
}

/**
 * License quota API functions
 * Uses SigV4-signed requests (IAM auth) to the API Gateway.
 */
export const licenseApi = {
  getQuota: async (): Promise<LicenseQuotaResponse> => {
    return sigv4Request<LicenseQuotaResponse>({ method: 'GET', url: buildUrl(ENDPOINTS.quota) })
  },
} as const
