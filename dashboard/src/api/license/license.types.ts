/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * License quota API types
 *
 * All timestamps are Unix epoch **seconds**.
 */

export type LicenseMode = 'free_tier' | 'license'

/**
 * Response from GET /v1/admin/licenses/latest/quota
 */
export interface LicenseQuotaResponse {
  mode: LicenseMode
  region: string
  total_limit: number
  used: number
  available: number
  expiration_timestamp?: number
}
