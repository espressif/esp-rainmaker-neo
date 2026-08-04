/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Background session keeper.
 *
 * A signed-in session rests on two clocks that expire ~1 hour apart from each other:
 * the Cognito access/id tokens, and the temporary AWS credentials minted from them.
 * The credentials can only be re-minted with live tokens, so renewal is strictly
 * ordered — tokens first, credentials second.
 *
 * Deliberately free of React so the engine can also be driven from non-component code,
 * and so `@/lib/auth` stays importable by modules that must not pull in React.
 */

import { getAccessTokenExpiryMs, refreshTokens } from './auth'
import { AUTH_STORAGE_KEYS } from './auth.constants'
import { getKeepSignedIn, getRefreshToken } from './auth.storage'
import {
  backoffDelayMs,
  earliestDeadlineMs,
  isRefreshDue,
  nextCheckDelayMs,
} from './session-schedule'
import { useAuthStore, type AwsCredentials } from '@/stores/auth.store'

export interface SessionKeeperDeps {
  /** Mints a fresh set of AWS credentials. Called only after the tokens are renewed. */
  refreshCredentials: () => Promise<AwsCredentials>
}

/**
 * `skipped` means the user never opted in, `terminal` means no future attempt can
 * succeed — both stop the keeper, while `transient` earns a backoff retry.
 */
export type ExtendOutcome = 'extended' | 'skipped' | 'transient-failure' | 'terminal-failure'

/** Dedupes overlapping renewals across the timer, the tab-visibility path and callers. */
let inFlight: Promise<ExtendOutcome> | null = null

/** Expiry of the cached AWS credentials, as epoch milliseconds. */
function credentialsExpiryMs(): number | null {
  const expiration = useAuthStore.getState().credentials?.expiration
  return expiration ? expiration * 1000 : null
}

/** Soonest point at which the session stops working, across both clocks. */
function sessionDeadlineMs(): number | null {
  return earliestDeadlineMs(getAccessTokenExpiryMs(), credentialsExpiryMs())
}

async function runExtend(deps: SessionKeeperDeps): Promise<ExtendOutcome> {
  if (!getKeepSignedIn() || !getRefreshToken()) {
    return 'skipped'
  }

  const refreshed = await refreshTokens()
  if (!refreshed.ok) {
    return refreshed.terminal ? 'terminal-failure' : 'transient-failure'
  }

  try {
    useAuthStore.getState().setCredentials(await deps.refreshCredentials())
    return 'extended'
  } catch (error) {
    console.warn('[Auth] Credential renewal failed', error)
    return 'transient-failure'
  }
}

/**
 * Renew the session now. Never throws and never surfaces anything to the user —
 * a failed renewal simply leaves the existing session to expire on its own terms.
 */
export async function extendSession(deps: SessionKeeperDeps): Promise<ExtendOutcome> {
  inFlight ??= runExtend(deps).finally(() => {
    inFlight = null
  })
  return inFlight
}

/**
 * Start renewing the session in the background. Returns the matching stop function.
 *
 * Safe to start and stop repeatedly (React 18 remounts effects in development):
 * teardown is symmetric and {@link extendSession} is single-flight.
 */
export function startSessionKeeper(deps: SessionKeeperDeps): () => void {
  let timer: ReturnType<typeof setTimeout> | null = null
  let consecutiveFailures = 0
  let stopped = false

  function arm(delayMs: number): void {
    if (stopped) {return}
    if (timer !== null) {clearTimeout(timer)}
    timer = setTimeout(() => void tick(), delayMs)
  }

  function armForDeadline(): void {
    arm(nextCheckDelayMs(sessionDeadlineMs(), Date.now()))
  }

  async function tick(): Promise<void> {
    if (stopped) {return}

    if (!getKeepSignedIn() || !getRefreshToken()) {
      stop()
      return
    }

    if (!isRefreshDue(sessionDeadlineMs(), Date.now())) {
      consecutiveFailures = 0
      armForDeadline()
      return
    }

    const outcome = await extendSession(deps)
    if (stopped) {return}

    // A revoked or expired refresh token cannot be recovered here; the route guard and
    // the 401 interceptor own the logout from this point.
    if (outcome === 'terminal-failure' || outcome === 'skipped') {
      stop()
      return
    }

    // Transient retries deliberately continue past expiry: the refresh token outlives
    // the access token, so a machine that slept for hours can still recover on wake.
    if (outcome === 'transient-failure') {
      consecutiveFailures += 1
      arm(backoffDelayMs(consecutiveFailures))
      return
    }

    consecutiveFailures = 0
    armForDeadline()
  }

  function handleVisibilityChange(): void {
    // Background tabs throttle timers and suspended machines skip them entirely, so
    // re-check against the real clock the moment the tab comes back.
    if (document.visibilityState === 'visible') {
      void tick()
    }
  }

  function handleStorage(event: StorageEvent): void {
    // `storage` fires only in the tabs that did not write, so this is another tab
    // announcing a renewal: re-arm against its new expiry instead of duplicating it.
    if (event.key === AUTH_STORAGE_KEYS.ACCESS_TOKEN) {
      consecutiveFailures = 0
      armForDeadline()
    }
  }

  function stop(): void {
    if (stopped) {return}
    stopped = true
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
    document.removeEventListener('visibilitychange', handleVisibilityChange)
    window.removeEventListener('storage', handleStorage)
  }

  document.addEventListener('visibilitychange', handleVisibilityChange)
  window.addEventListener('storage', handleStorage)
  void tick()

  return stop
}
