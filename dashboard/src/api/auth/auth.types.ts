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
}

/**
 * Request payload for changing password
 */
export interface ChangePasswordRequest {
  access_token: string
  /** Omitted for an admin who has no password yet; Cognito requires it otherwise. */
  old_password?: string
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

/**
 * A first authentication factor this app knows how to drive. Cognito can offer
 * others (`WEB_AUTHN`, `SMS_OTP`); those are filtered out before they reach the UI.
 */
export type ChallengeKind = 'EMAIL_OTP' | 'PASSWORD'

/**
 * Request payload for starting choice-based sign-in
 */
export interface StartSigninRequest {
  username: string
}

/**
 * What Cognito answers to the first `USER_AUTH` call.
 *
 * `select` is the normal answer and carries the factors this admin actually has.
 * `otpSent` happens when Cognito skips the menu and issues the challenge directly.
 * `unavailable` means the app client does not permit `USER_AUTH` — the stack update
 * has not landed — and the caller should fall back to password sign-in.
 */
export type StartSigninResult =
  | { kind: 'select'; session: string; challenges: ChallengeKind[] }
  | { kind: 'otpSent'; session: string; destination: string }
  | { kind: 'unavailable' }

/**
 * Request payload for choosing the email-OTP factor from the offered challenges
 */
export interface SelectOtpRequest {
  username: string
  session: string
}

/**
 * A live email-OTP challenge: the rotated session, and where Cognito says it sent
 * the code (already masked by Cognito, e.g. `a***@e***.com`).
 */
export interface OtpChallenge {
  session: string
  destination: string
}

/**
 * Request payload for answering an email-OTP challenge
 */
export interface VerifyOtpRequest {
  username: string
  session: string
  code: string
}

/**
 * The signed-in admin's configured first factors, from `GetUserAuthFactors`.
 * `hasPassword` is false for an admin seeded without one.
 */
export interface UserAuthFactors {
  hasPassword: boolean
}
