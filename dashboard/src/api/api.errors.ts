/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ApiErrorResponse } from './api.types'

/**
 * Custom API error class for typed error handling
 */
export class ApiError extends Error {
  public readonly code: string
  public readonly statusCode: number
  public readonly details?: unknown
  public readonly isApiError = true as const

  constructor(
    message: string,
    code: string,
    statusCode: number,
    details?: unknown
  ) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.statusCode = statusCode
    this.details = details

    // Maintains proper stack trace for where our error was thrown (V8 engines)
    if ('captureStackTrace' in Error && typeof Error.captureStackTrace === 'function') {
      Error.captureStackTrace(this, ApiError)
    }
  }

  /**
   * Create an ApiError from an axios error response
   */
  static fromAxiosError(error: unknown): ApiError {
    // Check if it's an axios error with response
    if (
      error &&
      typeof error === 'object' &&
      'response' in error &&
      error.response &&
      typeof error.response === 'object'
    ) {
      const response = error.response as {
        status?: number
        data?: ApiErrorResponse | string
      }

      const statusCode = response.status ?? 500

      // Try to extract error details from response data
      if (response.data && typeof response.data === 'object') {
        const data = response.data as ApiErrorResponse & { description?: string; error_code?: number }
        return new ApiError(
          data.message || data.description || 'An unexpected error occurred',
          data.code || (data.error_code != null ? String(data.error_code) : 'UNKNOWN_ERROR'),
          statusCode,
          data.details
        )
      }

      // Fallback for non-JSON responses
      return new ApiError(
        typeof response.data === 'string'
          ? response.data
          : 'An unexpected error occurred',
        'UNKNOWN_ERROR',
        statusCode
      )
    }

    // Network error or other non-response error
    if (error instanceof Error) {
      return new ApiError(
        error.message,
        'NETWORK_ERROR',
        0
      )
    }

    // Unknown error type
    return new ApiError(
      'An unexpected error occurred',
      'UNKNOWN_ERROR',
      0
    )
  }

  /**
   * Create an ApiError from a Zod validation error
   */
  static fromValidationError(message: string, details?: unknown): ApiError {
    return new ApiError(
      message,
      'VALIDATION_ERROR',
      422,
      details
    )
  }

  /**
   * Type guard to check if an error is an ApiError
   */
  static isApiError(error: unknown): error is ApiError {
    return (
      error instanceof ApiError ||
      (error !== null &&
        typeof error === 'object' &&
        'isApiError' in error &&
        error.isApiError === true)
    )
  }

  /**
   * Check if error is an authentication error (401/403)
   */
  isAuthError(): boolean {
    return this.statusCode === 401 || this.statusCode === 403
  }

  /**
   * Check if error is a not found error (404)
   */
  isNotFoundError(): boolean {
    return this.statusCode === 404
  }

  /**
   * Check if error is a validation error (400/422)
   */
  isValidationError(): boolean {
    return this.statusCode === 400 || this.statusCode === 422
  }

  /**
   * Check if error is a server error (5xx)
   */
  isServerError(): boolean {
    return this.statusCode >= 500 && this.statusCode < 600
  }

  /**
   * Convert to plain object for serialization
   */
  toJSON(): ApiErrorResponse & { statusCode: number } {
    return {
      code: this.code,
      message: this.message,
      statusCode: this.statusCode,
      details: this.details,
    }
  }
}

