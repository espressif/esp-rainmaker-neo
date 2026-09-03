/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Auth-domain storage helpers: tokens, sign-in preferences, and the OAuth
 * redirect hand-off. Raw storage access goes through `safe-storage`, so blocked
 * or absent storage degrades gracefully instead of throwing.
 */

import { safeLocalStorage, safeSessionStorage } from '../safe-storage'
import {
  AUTH_STORAGE_KEYS,
  LEGACY_AUTH_STORAGE_MIGRATIONS,
  PKCE_STORAGE_KEYS,
} from './auth.constants'

/**
 * Get the current access token
 */
export function getAccessToken(): string | null {
  return safeLocalStorage.get(AUTH_STORAGE_KEYS.ACCESS_TOKEN)
}

/**
 * Get the current ID token
 */
export function getIdToken(): string | null {
  return safeLocalStorage.get(AUTH_STORAGE_KEYS.ID_TOKEN)
}

/**
 * Get the current refresh token
 */
export function getRefreshToken(): string | null {
  return safeLocalStorage.get(AUTH_STORAGE_KEYS.REFRESH_TOKEN)
}

/**
 * Store auth tokens received from token exchange or login
 */
export function storeAuthTokens(tokens: {
  accessToken?: string
  idToken?: string
  refreshToken?: string
}): void {
  if (tokens.accessToken) {
    safeLocalStorage.set(AUTH_STORAGE_KEYS.ACCESS_TOKEN, tokens.accessToken)
  }
  if (tokens.idToken) {
    safeLocalStorage.set(AUTH_STORAGE_KEYS.ID_TOKEN, tokens.idToken)
  }
  if (tokens.refreshToken) {
    safeLocalStorage.set(AUTH_STORAGE_KEYS.REFRESH_TOKEN, tokens.refreshToken)
  }
}

/**
 * Store "keep me signed in" preference
 */
export function storeKeepSignedIn(value: boolean): void {
  safeLocalStorage.set(AUTH_STORAGE_KEYS.KEEP_SIGNED_IN, value.toString())
}

/**
 * Get "keep me signed in" preference
 */
export function getKeepSignedIn(): boolean {
  return safeLocalStorage.get(AUTH_STORAGE_KEYS.KEEP_SIGNED_IN) === 'true'
}

/**
 * Store the address that last signed in successfully, so the login entry screen can
 * greet the returning admin instead of asking for the email again.
 */
export function storeLastLoginEmail(email: string): void {
  safeLocalStorage.set(AUTH_STORAGE_KEYS.LAST_LOGIN_EMAIL, email)
}

/**
 * Get the address that last signed in successfully, or null when none is stored
 * (first visit, cleared storage, or unusable localStorage).
 */
export function getLastLoginEmail(): string | null {
  return safeLocalStorage.get(AUTH_STORAGE_KEYS.LAST_LOGIN_EMAIL)
}

/**
 * Clear all auth tokens from storage.
 *
 * "Keep me signed in" deliberately survives: it is a per-browser preference, not a
 * credential, and it prefills the login checkbox on the next visit. It cannot extend
 * anything on its own — the session keeper also requires the refresh token, which is
 * removed here. The last-login email survives too: the remembered-account screen
 * exists precisely for the admin who just logged out.
 */
export function clearAuthTokens(): void {
  safeLocalStorage.remove(AUTH_STORAGE_KEYS.ACCESS_TOKEN)
  safeLocalStorage.remove(AUTH_STORAGE_KEYS.ID_TOKEN)
  safeLocalStorage.remove(AUTH_STORAGE_KEYS.REFRESH_TOKEN)
}

/**
 * One-time move of auth storage from the pre-prefix key names to the
 * `storagePrefix`-namespaced ones, so the rename does not log every deployed
 * browser out. Runs at startup, before anything reads a token. A value already
 * present under the new key wins — the legacy copy is stale by definition —
 * and the legacy entry is removed either way.
 */
export function migrateLegacyAuthStorage(): void {
  for (const [legacyKey, currentKey] of LEGACY_AUTH_STORAGE_MIGRATIONS) {
    const value = safeLocalStorage.get(legacyKey)
    if (value === null) {
      continue
    }
    if (safeLocalStorage.get(currentKey) === null) {
      safeLocalStorage.set(currentKey, value)
    }
    safeLocalStorage.remove(legacyKey)
  }
}

/**
 * Get and clear stored redirect path
 */
export function consumeRedirectPath(): string | null {
  const path = safeSessionStorage.get(PKCE_STORAGE_KEYS.REDIRECT_PATH)
  if (path) {
    safeSessionStorage.remove(PKCE_STORAGE_KEYS.REDIRECT_PATH)
  }
  return path
}
