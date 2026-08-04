/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Auth-related constants
 * Centralizes all storage keys and configuration
 */

/**
 * localStorage keys for auth tokens
 */
export const AUTH_STORAGE_KEYS = {
  ACCESS_TOKEN: 'access_token',
  ID_TOKEN: 'id_token',
  REFRESH_TOKEN: 'refresh_token',
  PROFILE: 'adminProfile',
  KEEP_SIGNED_IN: 'keep_signed_in',
} as const

/**
 * sessionStorage keys for OAuth PKCE flow
 */
export const PKCE_STORAGE_KEYS = {
  VERIFIER: 'pkce_verifier',
  STATE: 'pkce_state',
  REDIRECT_PATH: 'oauth_redirect_path',
} as const

/**
 * Tuning for the background session keeper (see `session-schedule.ts`).
 *
 * Cognito tokens and the AWS credentials derived from them both last ~1 hour, so the
 * keeper renews them a couple of minutes ahead of whichever expires first.
 */
export const SESSION_REFRESH = {
  /** Renew this far ahead of expiry. */
  LEAD_MS: 2 * 60 * 1000,
  /** Floor for a scheduled check, so a past-due deadline cannot spin the timer. */
  MIN_DELAY_MS: 5 * 1000,
  /**
   * Ceiling for a single `setTimeout`. Long timers do not fire on schedule after the
   * machine sleeps and are throttled in background tabs, so the keeper re-arms and
   * re-reads the wall clock instead of trusting one long delay.
   */
  MAX_REARM_MS: 10 * 60 * 1000,
  /** Spread across tabs so several do not attempt the same renewal in one second. */
  JITTER_MS: 10 * 1000,
  /** First retry delay after a transient failure; doubles up to the ceiling. */
  BACKOFF_BASE_MS: 30 * 1000,
  BACKOFF_MAX_MS: 5 * 60 * 1000,
  /** Treat a token as expired this early, to absorb client/server clock drift. */
  CLOCK_SKEW_MS: 30 * 1000,
} as const

