/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Shared API types for the admin dashboard
 */

/**
 * Standard API error response shape from the backend
 */
export interface ApiErrorResponse {
  code: string
  message: string
  details?: unknown
}

/**
 * Generic paginated response wrapper
 */
export interface PaginatedResponse<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
  hasMore: boolean
}

/**
 * Standard API response wrapper for single items
 */
export interface ApiResponse<T> {
  data: T
  message?: string
}

/**
 * HTTP methods supported by the API
 */
export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

/**
 * Request configuration options
 */
export interface RequestConfig {
  /** Custom headers to merge with defaults */
  headers?: Record<string, string>
  /** Query parameters */
  params?: Record<string, string | number | boolean | undefined>
  /** Request timeout in milliseconds */
  timeout?: number
  /** Whether to include credentials */
  withCredentials?: boolean
}

