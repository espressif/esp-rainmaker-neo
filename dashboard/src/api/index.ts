/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * API module exports
 *
 * Usage:
 * ```ts
 * import { useSignin, useGetUserCreds } from '@/api'
 * import type { SigninResponse, UserCredsResponse } from '@/api'
 * ```
 */

// Core infrastructure
export { httpClient, resetLogoutFlag } from './http-client'
export { ApiError } from './api.errors'
export type {
  ApiErrorResponse,
  ApiResponse,
  PaginatedResponse,
  HttpMethod,
  RequestConfig,
} from './api.types'

// Auth domain
export * from './auth'

// User domain
export * from './user'

// Node tags domain
export * from './node-tags'

// Node registration domain
export * from './node-registration'

// License management domain
export * from './license'

// OTA jobs domain
export * from './ota-jobs'

// OTA images domain
export * from './ota-images'

// Node groups domain
export * from './node-groups'

// Runtime config domain
export * from './config'

// Post-deployment domain
export * from './post-deployment'
