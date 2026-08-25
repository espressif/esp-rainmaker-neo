/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { sdkRangeSatisfied } from './sdk-range'

describe('sdkRangeSatisfied', () => {
  it('matches exact versions, treating omitted parts as 0', () => {
    expect(sdkRangeSatisfied('1.0.0', '1.0.0')).toBe(true)
    expect(sdkRangeSatisfied('1.0', '1.0.0')).toBe(true)
    expect(sdkRangeSatisfied('1', '1.0.0')).toBe(true)
    expect(sdkRangeSatisfied('1.0.0', '1.0.1')).toBe(false)
    expect(sdkRangeSatisfied('1.0.1', '1.0.0')).toBe(false)
  })

  it('satisfies caret ranges when the host is at or above the wanted minor/patch', () => {
    expect(sdkRangeSatisfied('^1.0', '1.0.0')).toBe(true)
    expect(sdkRangeSatisfied('^1.0', '1.4.2')).toBe(true)
    expect(sdkRangeSatisfied('^1.2.3', '1.2.3')).toBe(true)
    expect(sdkRangeSatisfied('^1.2.3', '1.3.0')).toBe(true)
    expect(sdkRangeSatisfied('^1', '1.9.9')).toBe(true)
  })

  it('refuses caret ranges the host is below', () => {
    expect(sdkRangeSatisfied('^1.2', '1.1.9')).toBe(false)
    expect(sdkRangeSatisfied('^1.2.3', '1.2.2')).toBe(false)
  })

  it('refuses across majors in both directions', () => {
    expect(sdkRangeSatisfied('^1.0', '2.0.0')).toBe(false)
    expect(sdkRangeSatisfied('^2.0', '1.9.0')).toBe(false)
  })

  it('refuses malformed ranges and versions', () => {
    expect(sdkRangeSatisfied('', '1.0.0')).toBe(false)
    expect(sdkRangeSatisfied('^one.0', '1.0.0')).toBe(false)
    expect(sdkRangeSatisfied('^1.0', 'not-a-version')).toBe(false)
    expect(sdkRangeSatisfied('>=1.0', '1.0.0')).toBe(false)
  })
})
