/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { SESSION_REFRESH } from './auth.constants'
import {
  backoffDelayMs,
  earliestDeadlineMs,
  isRefreshDue,
  nextCheckDelayMs,
} from './session-schedule'

/** Pinned jitter so the clamping assertions are exact. */
const noJitter = () => 0

const NOW = 1_700_000_000_000

describe('earliestDeadlineMs', () => {
  it('picks the soonest known deadline', () => {
    expect(earliestDeadlineMs(NOW + 5000, NOW + 1000)).toBe(NOW + 1000)
  })

  it('ignores deadlines it cannot read', () => {
    expect(earliestDeadlineMs(null, NOW + 1000, undefined)).toBe(NOW + 1000)
    expect(earliestDeadlineMs(NaN, NOW + 1000)).toBe(NOW + 1000)
  })

  it('returns null when nothing is known', () => {
    expect(earliestDeadlineMs()).toBeNull()
    expect(earliestDeadlineMs(null, undefined)).toBeNull()
  })
})

describe('isRefreshDue', () => {
  it('is due once inside the lead window', () => {
    expect(isRefreshDue(NOW + SESSION_REFRESH.LEAD_MS, NOW)).toBe(true)
    expect(isRefreshDue(NOW + SESSION_REFRESH.LEAD_MS - 1, NOW)).toBe(true)
  })

  it('is not due while the deadline is further out than the lead', () => {
    expect(isRefreshDue(NOW + SESSION_REFRESH.LEAD_MS + 1, NOW)).toBe(false)
  })

  it('stays due after the deadline has passed', () => {
    expect(isRefreshDue(NOW - 60_000, NOW)).toBe(true)
  })

  it('is never due without a deadline', () => {
    expect(isRefreshDue(null, NOW)).toBe(false)
  })
})

describe('nextCheckDelayMs', () => {
  it('waits until the lead window opens', () => {
    const deadline = NOW + SESSION_REFRESH.LEAD_MS + 60_000
    expect(nextCheckDelayMs(deadline, NOW, noJitter)).toBe(60_000)
  })

  it('caps long waits so the timer re-reads the clock after a suspend', () => {
    const deadline = NOW + 60 * 60 * 1000
    expect(nextCheckDelayMs(deadline, NOW, noJitter)).toBe(SESSION_REFRESH.MAX_REARM_MS)
  })

  it('floors past-due deadlines instead of scheduling in the past', () => {
    expect(nextCheckDelayMs(NOW - 60_000, NOW, noJitter)).toBe(SESSION_REFRESH.MIN_DELAY_MS)
  })

  it('falls back to the re-arm ceiling when the deadline is unknown', () => {
    expect(nextCheckDelayMs(null, NOW, noJitter)).toBe(SESSION_REFRESH.MAX_REARM_MS)
  })

  it('adds bounded jitter so tabs do not renew in lockstep', () => {
    const deadline = NOW + SESSION_REFRESH.LEAD_MS + 60_000
    expect(nextCheckDelayMs(deadline, NOW, () => 0.5)).toBe(
      60_000 + SESSION_REFRESH.JITTER_MS / 2,
    )
    expect(nextCheckDelayMs(deadline, NOW, () => 0.999)).toBeLessThan(
      60_000 + SESSION_REFRESH.JITTER_MS,
    )
  })
})

describe('backoffDelayMs', () => {
  it('doubles from the base delay', () => {
    expect(backoffDelayMs(1)).toBe(SESSION_REFRESH.BACKOFF_BASE_MS)
    expect(backoffDelayMs(2)).toBe(SESSION_REFRESH.BACKOFF_BASE_MS * 2)
    expect(backoffDelayMs(3)).toBe(SESSION_REFRESH.BACKOFF_BASE_MS * 4)
  })

  it('never exceeds the ceiling', () => {
    expect(backoffDelayMs(50)).toBe(SESSION_REFRESH.BACKOFF_MAX_MS)
  })

  it('treats a zeroth attempt as the first', () => {
    expect(backoffDelayMs(0)).toBe(SESSION_REFRESH.BACKOFF_BASE_MS)
  })
})
