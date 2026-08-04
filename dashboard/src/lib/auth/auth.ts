/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Core authentication utilities
 * Token refresh, auth status checks, and logout
 */

import {
  getAccessToken,
  getIdToken,
  getRefreshToken,
  storeAuthTokens,
  clearAuthTokens,
} from './auth.storage'
import { InitiateAuthCommand } from '@aws-sdk/client-cognito-identity-provider'
import { getCognitoClient } from './cognito-client'
import { SESSION_REFRESH } from './auth.constants'
import { getCognitoClientId } from '../config'
import { useAuthStore } from '@/stores/auth.store'
import { useUserStore } from '@/stores/user.store'

/**
 * Decode JWT payload without verification
 * Used only for reading claims locally (expiration checks, profile display)
 */
function decodeJwtPayload<T = Record<string, unknown>>(token: string): T | null {
  try {
    const parts = token.split('.')
    if (parts.length !== 3) {return null}
    // Unverified by design (see above) — the caller names the claims it expects.
    return JSON.parse(atob(parts[1])) as T
  } catch {
    return null
  }
}

/**
 * Read a token's `exp` claim as epoch milliseconds. Null when absent or malformed.
 */
function tokenExpiryMs(token: string): number | null {
  const payload = decodeJwtPayload<{ exp?: number }>(token)
  return payload?.exp ? payload.exp * 1000 : null
}

/**
 * Check if a token is expired, treating it as expired a little early so a client clock
 * running behind the server cannot hand out a token the server has already rejected.
 */
function isTokenExpired(token: string): boolean {
  const expiresAt = tokenExpiryMs(token)
  if (expiresAt === null) {return true}
  return Date.now() >= expiresAt - SESSION_REFRESH.CLOCK_SKEW_MS
}

/**
 * Expiry of the stored access token, as epoch milliseconds.
 * Null when there is no token or it carries no `exp` claim.
 */
export function getAccessTokenExpiryMs(): number | null {
  const token = getAccessToken()
  return token ? tokenExpiryMs(token) : null
}

/**
 * Identity claims carried by the Cognito id_token.
 * All fields optional — the token shape is controlled by the user pool, not this app.
 */
export interface IdTokenClaims {
  sub?: string
  email?: string
  email_verified?: boolean
  'cognito:username'?: string
  auth_time?: number
  iss?: string
  exp?: number
}

/**
 * Decode the stored Cognito id_token locally (no signature verification).
 * Returns null when the token is absent or malformed.
 */
export function getIdTokenClaims(): IdTokenClaims | null {
  const token = getIdToken()
  return token ? decodeJwtPayload<IdTokenClaims>(token) : null
}

/**
 * Check if user is authenticated
 * Uses local token validation first to avoid network latency
 * Only makes API call if token appears expired (to attempt refresh)
 */
export async function checkAuthStatus(): Promise<boolean> {
  const token = getAccessToken()
  if (!token) {
    return false
  }

  // Fast path: if token exists and is not expired, trust it
  if (!isTokenExpired(token)) {
    return true
  }

  // Token appears expired - try to refresh
  const refreshed = await refreshAccessToken()

  if (refreshed) {
    const newToken = getAccessToken()
    if (newToken && !isTokenExpired(newToken)) {
      return true
    }
  }

  return false
}

/**
 * Outcome of a token refresh.
 *
 * `terminal` separates "this will never work again" (the refresh token was revoked or
 * has outlived its own TTL) from a transient network or service failure, so a caller
 * driving a retry loop knows whether retrying is pointless.
 */
export type RefreshResult = { ok: true } | { ok: false; terminal: boolean }

/** Cognito's error name when the refresh token is no longer usable. */
const REFRESH_TOKEN_REJECTED = 'NotAuthorizedException'

/**
 * Refresh the access + id tokens using the stored refresh token.
 */
export async function refreshTokens(): Promise<RefreshResult> {
  const refreshToken = getRefreshToken()
  if (!refreshToken) {
    console.warn('[Auth] No refresh token available')
    return { ok: false, terminal: true }
  }

  try {
    const response = await getCognitoClient().send(
      new InitiateAuthCommand({
        AuthFlow: 'REFRESH_TOKEN_AUTH',
        ClientId: getCognitoClientId(),
        AuthParameters: {
          REFRESH_TOKEN: refreshToken,
        },
      }),
    )

    const result = response.AuthenticationResult
    if (!result?.AccessToken) {
      console.error('[Auth] Token refresh failed: no access token returned')
      return { ok: false, terminal: false }
    }

    // REFRESH_TOKEN_AUTH does not return a new refresh token — preserve the
    // existing one and only update the access + id tokens.
    storeAuthTokens({
      accessToken: result.AccessToken,
      idToken: result.IdToken,
      refreshToken,
    })

    return { ok: true }
  } catch (error) {
    console.error('[Auth] Token refresh error:', error)
    const name = (error as { name?: string })?.name
    return { ok: false, terminal: name === REFRESH_TOKEN_REJECTED }
  }
}

/**
 * Refresh the access token using the refresh token.
 * Boolean-only view of {@link refreshTokens} for callers that just retry once.
 */
export async function refreshAccessToken(): Promise<boolean> {
  return (await refreshTokens()).ok
}

/** Where {@link logout} sends the browser when the caller has no preference. */
const DEFAULT_LOGOUT_REDIRECT = '/login'

/**
 * Logout the current user: clears tokens and hard-navigates away.
 *
 * A full page load (rather than a router navigation) is deliberate — it discards the
 * query cache and every store the signed-in session populated. `redirectTo` lets a
 * caller land on `/login` with a banner param, e.g. after a self-service password change.
 */
export function logout(redirectTo: string = DEFAULT_LOGOUT_REDIRECT): void {
  clearAuthTokens()
  useAuthStore.getState().clearCredentials()
  useUserStore.getState().clearUser()
  window.location.href = redirectTo
}

/**
 * Handle API authentication errors
 * Logs out user on 401/403
 */
export function handleAuthError(error: unknown): never {
  const status = (error as { response?: { status?: number } })?.response?.status

  if (status === 401 || status === 403) {
    console.warn('[Auth] Authentication expired. Logging out.')
    logout()
  }

  throw error
}
