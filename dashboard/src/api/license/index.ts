/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * License quota API module exports
 */

export type { LicenseMode, LicenseQuotaResponse } from './license.types'

export { licenseApi } from './license.api'

export { licenseKeys, licenseQueries, useGetQuota } from './license.queries'
