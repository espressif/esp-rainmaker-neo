/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Auth-related constants
 * Centralizes all storage keys and configuration
 */

// Relative import (not `@/…`) because this module is reached from the alias-free
// `vitest.config.ts` via `auth.storage.ts` and the sign-in flow store.
import { appStorageKey } from '../app-config'

/**
 * localStorage keys for auth tokens and sign-in preferences, all namespaced by the
 * app-wide `storagePrefix` like the persisted Zustand stores.
 */
export const AUTH_STORAGE_KEYS = {
  ACCESS_TOKEN: appStorageKey('access_token'),
  ID_TOKEN: appStorageKey('id_token'),
  REFRESH_TOKEN: appStorageKey('refresh_token'),
  KEEP_SIGNED_IN: appStorageKey('keep_signed_in'),
  LAST_LOGIN_EMAIL: appStorageKey('last_login_email'),
} as const

/**
 * Where each value lived before the keys were prefixed (2026-09). The startup
 * migration (`migrateLegacyAuthStorage`) moves these so deployed browsers keep
 * their sessions across the rename. Delete the table (and the migration) once
 * every active browser has passed through a build that contains it.
 */
export const LEGACY_AUTH_STORAGE_MIGRATIONS: ReadonlyArray<
  readonly [legacyKey: string, currentKey: string]
> = [
  ['access_token', AUTH_STORAGE_KEYS.ACCESS_TOKEN],
  ['id_token', AUTH_STORAGE_KEYS.ID_TOKEN],
  ['refresh_token', AUTH_STORAGE_KEYS.REFRESH_TOKEN],
  ['keep_signed_in', AUTH_STORAGE_KEYS.KEEP_SIGNED_IN],
]

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

