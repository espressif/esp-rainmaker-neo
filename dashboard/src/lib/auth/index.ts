/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Auth module exports
 *
 * Usage:
 * ```ts
 * import { checkAuthStatus, logout, getAccessToken } from '@/lib/auth'
 * ```
 */

// Constants
export {
  AUTH_STORAGE_KEYS,
  SESSION_REFRESH,
} from './auth.constants'

// Storage utilities
export {
  getAccessToken,
  getIdToken,
  getRefreshToken,
  storeAuthTokens,
  clearAuthTokens,
  storeKeepSignedIn,
  getKeepSignedIn,
  consumeRedirectPath,
} from './auth.storage'

// Core auth functions
export {
  checkAuthStatus,
  refreshAccessToken,
  refreshTokens,
  getAccessTokenExpiryMs,
  logout,
  handleAuthError,
  getIdTokenClaims,
} from './auth'
export type { IdTokenClaims, RefreshResult } from './auth'

// Background session renewal
export { extendSession, startSessionKeeper } from './session-manager'
export type { ExtendOutcome, SessionKeeperDeps } from './session-manager'
export {
  backoffDelayMs,
  earliestDeadlineMs,
  isRefreshDue,
  nextCheckDelayMs,
} from './session-schedule'

// Route guards
export { requireAuth } from './require-auth'

// Post-login redirect
export {
  loginHrefWithRedirect,
  loginRedirectSearch,
  LOGIN_REDIRECT_PARAM,
} from './login-redirect'

// Password flow failure messages (reset, and self-service change)
export {
  requestCodeErrorMessage,
  confirmResetErrorMessage,
  changePasswordErrorMessage,
  isIncorrectCurrentPasswordError,
  SESSION_EXPIRED_MESSAGE,
} from './password-errors'
export type { LocalizedMessage } from './password-errors'

// PKCE / CSRF-state generation for authorization-code requests
export {
  generateCodeVerifier,
  generateCodeChallenge,
  generateState,
  generatePkceParams,
} from './auth.pkce'
