/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import axios, {
  type AxiosError,
  type AxiosInstance,
  type InternalAxiosRequestConfig,
} from 'axios'
import { refreshAccessToken, logout, getAccessToken, getIdToken } from '@/lib/auth'
import { getEspUserApiUrl } from '@/lib/config'
import { ApiError } from './api.errors'

/**
 * A request config carrying our own single-retry marker.
 *
 * `_retry` is set once a 401 has triggered a token refresh, so a second 401 on
 * the replayed request falls through to logout instead of looping forever.
 */
interface RetriableRequestConfig extends InternalAxiosRequestConfig {
  _retry?: boolean
}

// Flag to prevent multiple simultaneous refresh attempts
let isRefreshing = false
let isLoggingOut = false
let failedQueue: Array<{
  resolve: (value?: unknown) => void
  reject: (reason?: unknown) => void
}> = []

/**
 * Process queued requests after token refresh
 */
const processQueue = (error: unknown = null): void => {
  failedQueue.forEach(prom => {
    if (error) {
      prom.reject(error)
    } else {
      prom.resolve()
    }
  })
  failedQueue = []
}

/**
 * Create and configure the axios instance
 */
function createHttpClient(): AxiosInstance {
  const instance = axios.create({
    timeout: 30000,
    headers: {
      'Content-Type': 'application/json',
    },
  })

  // Request interceptor: Add auth token and base URL
  instance.interceptors.request.use(
    (config: InternalAxiosRequestConfig) => {
      // Set base URL dynamically (allows runtime config)
      if (!config.baseURL) {
        config.baseURL = getEspUserApiUrl()
      }

      // Add ID token in Authorization header (Cognito authorizer validates ID
      // tokens). No try/catch: `getIdToken` goes through safe-storage and
      // returns null instead of throwing when storage is unusable.
      const token = getIdToken()
      if (token && config.headers) {
        config.headers['Authorization'] = `Bearer ${token}`
      }

      return config
    },
    (error) => Promise.reject(ApiError.fromAxiosError(error))
  )

  // Response interceptor: Handle 401 with token refresh
  instance.interceptors.response.use(
    (response) => response,
    async (error: AxiosError) => {
      const originalRequest = error.config as RetriableRequestConfig | undefined

      // If already logging out, reject immediately
      if (isLoggingOut) {
        return Promise.reject(ApiError.fromAxiosError(error))
      }

      // Check if error is 401 and we haven't already tried to refresh
      if (error.response?.status === 401 && originalRequest && !originalRequest._retry) {
        if (isRefreshing) {
          // If already refreshing, queue this request
          return new Promise((resolve, reject) => {
            failedQueue.push({ resolve, reject })
          })
            .then(() => instance(originalRequest))
            .catch(err => Promise.reject(ApiError.fromAxiosError(err)))
        }

        originalRequest._retry = true
        isRefreshing = true

        try {
          const refreshSucceeded = await refreshAccessToken()

          if (refreshSucceeded) {
            // Update the authorization header with new token
            const newToken = getAccessToken()
            if (newToken) {
              originalRequest.headers['Authorization'] = `Bearer ${newToken}`
            }

            processQueue()
            isRefreshing = false

            // Retry the original request
            return instance(originalRequest)
          } else {
            // Refresh failed, logout user
            processQueue(error)
            isRefreshing = false
            isLoggingOut = true
            logout()
            return Promise.reject(ApiError.fromAxiosError(error))
          }
        } catch (refreshError) {
          processQueue(refreshError)
          isRefreshing = false
          isLoggingOut = true
          logout()
          return Promise.reject(ApiError.fromAxiosError(refreshError))
        }
      }

      return Promise.reject(ApiError.fromAxiosError(error))
    }
  )

  return instance
}

/**
 * Configured axios instance for API calls
 * - Automatically injects auth token
 * - Handles 401 with token refresh
 * - Converts errors to ApiError
 */
export const httpClient = createHttpClient()

/**
 * Reset the logout flag (call after successful login)
 */
export function resetLogoutFlag(): void {
  isLoggingOut = false
}

