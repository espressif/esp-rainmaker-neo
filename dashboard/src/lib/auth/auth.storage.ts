/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Safe storage utilities for auth tokens
 * Handles storage errors gracefully (private browsing, quota exceeded, etc.)
 */

import { AUTH_STORAGE_KEYS, PKCE_STORAGE_KEYS } from './auth.constants'

type AuthStorageKey = typeof AUTH_STORAGE_KEYS[keyof typeof AUTH_STORAGE_KEYS]
type PkceStorageKey = typeof PKCE_STORAGE_KEYS[keyof typeof PKCE_STORAGE_KEYS]

/**
 * Safely get item from localStorage
 */
export function getLocalStorageItem(key: AuthStorageKey): string | null {
  try {
    return localStorage.getItem(key)
  } catch (error) {
    console.warn(`[Auth] Failed to read from localStorage: ${key}`, error)
    return null
  }
}

/**
 * Safely set item in localStorage
 */
export function setLocalStorageItem(key: AuthStorageKey, value: string): boolean {
  try {
    localStorage.setItem(key, value)
    return true
  } catch (error) {
    console.warn(`[Auth] Failed to write to localStorage: ${key}`, error)
    return false
  }
}

/**
 * Safely remove item from localStorage
 */
export function removeLocalStorageItem(key: AuthStorageKey): boolean {
  try {
    localStorage.removeItem(key)
    return true
  } catch (error) {
    console.warn(`[Auth] Failed to remove from localStorage: ${key}`, error)
    return false
  }
}

/**
 * Safely get item from sessionStorage
 */
export function getSessionStorageItem(key: PkceStorageKey): string | null {
  try {
    return sessionStorage.getItem(key)
  } catch (error) {
    console.warn(`[Auth] Failed to read from sessionStorage: ${key}`, error)
    return null
  }
}

/**
 * Safely set item in sessionStorage
 */
export function setSessionStorageItem(key: PkceStorageKey, value: string): boolean {
  try {
    sessionStorage.setItem(key, value)
    return true
  } catch (error) {
    console.warn(`[Auth] Failed to write to sessionStorage: ${key}`, error)
    return false
  }
}

/**
 * Safely remove item from sessionStorage
 */
export function removeSessionStorageItem(key: PkceStorageKey): boolean {
  try {
    sessionStorage.removeItem(key)
    return true
  } catch (error) {
    console.warn(`[Auth] Failed to remove from sessionStorage: ${key}`, error)
    return false
  }
}

/**
 * Get the current access token
 */
export function getAccessToken(): string | null {
  return getLocalStorageItem(AUTH_STORAGE_KEYS.ACCESS_TOKEN)
}

/**
 * Get the current ID token
 */
export function getIdToken(): string | null {
  return getLocalStorageItem(AUTH_STORAGE_KEYS.ID_TOKEN)
}

/**
 * Get the current refresh token
 */
export function getRefreshToken(): string | null {
  return getLocalStorageItem(AUTH_STORAGE_KEYS.REFRESH_TOKEN)
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
    setLocalStorageItem(AUTH_STORAGE_KEYS.ACCESS_TOKEN, tokens.accessToken)
  }
  if (tokens.idToken) {
    setLocalStorageItem(AUTH_STORAGE_KEYS.ID_TOKEN, tokens.idToken)
  }
  if (tokens.refreshToken) {
    setLocalStorageItem(AUTH_STORAGE_KEYS.REFRESH_TOKEN, tokens.refreshToken)
  }
}

/**
 * Store "keep me signed in" preference
 */
export function storeKeepSignedIn(value: boolean): void {
  setLocalStorageItem(AUTH_STORAGE_KEYS.KEEP_SIGNED_IN, value.toString())
}

/**
 * Get "keep me signed in" preference
 */
export function getKeepSignedIn(): boolean {
  return getLocalStorageItem(AUTH_STORAGE_KEYS.KEEP_SIGNED_IN) === 'true'
}

/**
 * Clear all auth tokens from storage.
 *
 * "Keep me signed in" deliberately survives: it is a per-browser preference, not a
 * credential, and it prefills the login checkbox on the next visit. It cannot extend
 * anything on its own — the session keeper also requires the refresh token, which is
 * removed here.
 */
export function clearAuthTokens(): void {
  removeLocalStorageItem(AUTH_STORAGE_KEYS.ACCESS_TOKEN)
  removeLocalStorageItem(AUTH_STORAGE_KEYS.ID_TOKEN)
  removeLocalStorageItem(AUTH_STORAGE_KEYS.REFRESH_TOKEN)
  removeLocalStorageItem(AUTH_STORAGE_KEYS.PROFILE)
}

/**
 * Get and clear stored redirect path
 */
export function consumeRedirectPath(): string | null {
  const path = getSessionStorageItem(PKCE_STORAGE_KEYS.REDIRECT_PATH)
  if (path) {
    removeSessionStorageItem(PKCE_STORAGE_KEYS.REDIRECT_PATH)
  }
  return path
}

