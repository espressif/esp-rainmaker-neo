/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Timing maths for the background session keeper.
 *
 * Kept pure and DOM-free so the part that is easy to get wrong — and impossible to
 * observe in a running app without waiting an hour — can be unit tested directly.
 */

import { SESSION_REFRESH } from './auth.constants'

/**
 * Earliest of the deadlines the session depends on, ignoring the ones we cannot read.
 * Returns null when none is known.
 */
export function earliestDeadlineMs(
  ...deadlines: (number | null | undefined)[]
): number | null {
  const known = deadlines.filter(
    (deadline): deadline is number => typeof deadline === 'number' && Number.isFinite(deadline),
  )
  return known.length > 0 ? Math.min(...known) : null
}

/**
 * Whether the session is close enough to its deadline to renew now.
 * An unknown deadline is not due — there is nothing to schedule against.
 */
export function isRefreshDue(deadlineMs: number | null, nowMs: number): boolean {
  if (deadlineMs === null) {return false}
  return nowMs >= deadlineMs - SESSION_REFRESH.LEAD_MS
}

/**
 * Delay until the next check.
 *
 * Capped at {@link SESSION_REFRESH.MAX_REARM_MS} rather than sleeping the full hour:
 * a long `setTimeout` does not fire on schedule once the machine suspends, so the
 * keeper wakes periodically and compares against the real clock instead.
 *
 * `random` is injectable purely so tests can pin the jitter.
 */
export function nextCheckDelayMs(
  deadlineMs: number | null,
  nowMs: number,
  random: () => number = Math.random,
): number {
  const untilDue =
    deadlineMs === null
      ? SESSION_REFRESH.MAX_REARM_MS
      : deadlineMs - SESSION_REFRESH.LEAD_MS - nowMs

  const clamped = Math.min(
    Math.max(untilDue, SESSION_REFRESH.MIN_DELAY_MS),
    SESSION_REFRESH.MAX_REARM_MS,
  )

  return clamped + Math.floor(random() * SESSION_REFRESH.JITTER_MS)
}

/**
 * Delay before retrying after a transient failure. `attempt` is 1-based.
 */
export function backoffDelayMs(attempt: number): number {
  const steps = Math.max(attempt, 1) - 1
  const delay = SESSION_REFRESH.BACKOFF_BASE_MS * 2 ** steps
  return Math.min(delay, SESSION_REFRESH.BACKOFF_MAX_MS)
}
