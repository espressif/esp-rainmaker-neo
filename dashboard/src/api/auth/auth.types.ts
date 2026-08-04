/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Auth API types
 */

/**
 * Request payload for user signin
 */
export interface SigninRequest {
  username: string
  password: string
}

/**
 * Successful signin response
 */
export interface SigninResponse {
  access_token: string
  refresh_token: string
  id_token: string
  token_type: string
  must_change_password?: boolean
}

/**
 * Request payload for changing password
 */
export interface ChangePasswordRequest {
  access_token: string
  old_password: string
  new_password: string
}

/**
 * Successful change password response
 */
export interface ChangePasswordResponse {
  message: string
  success: boolean
}

/**
 * Request payload for starting a password reset (emails a confirmation code)
 */
export interface ForgotPasswordRequest {
  username: string
}

/**
 * Result of requesting a password reset code.
 *
 * Deliberately carries no delivery details: the response is identical for
 * registered and unknown addresses so the form cannot be used to enumerate
 * admin accounts.
 */
export interface ForgotPasswordResponse {
  message: string
  success: boolean
}

/**
 * Request payload for completing a password reset with the emailed code
 */
export interface ConfirmForgotPasswordRequest {
  username: string
  code: string
  new_password: string
}

/**
 * Successful password reset response
 */
export interface ConfirmForgotPasswordResponse {
  message: string
  success: boolean
}
