/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { findReleaseMismatch } from './release-version'

describe('findReleaseMismatch', () => {
  it('returns null when every required stack reports the plugins release', () => {
    expect(findReleaseMismatch(['a', 'b'], { a: '0.8.1', b: '0.8.1' }, '0.8.1')).toBeNull()
  })

  it('names the first required stack on a different release', () => {
    expect(findReleaseMismatch(['a', 'b'], { a: '0.8.1', b: '0.8.0' }, '0.8.1')).toEqual({
      stack: 'b',
      stackVersion: '0.8.0',
    })
  })

  it('ignores stacks the feature does not require', () => {
    expect(findReleaseMismatch(['a'], { a: '0.8.1', other: '0.7.0' }, '0.8.1')).toBeNull()
  })

  it('does not compare a stack that reports no version or an unknown one', () => {
    expect(findReleaseMismatch(['a', 'b'], { b: 'unknown' }, '0.8.1')).toBeNull()
  })

  it('does not compare when the plugins carry no version or an unknown one', () => {
    expect(findReleaseMismatch(['a'], { a: '0.8.0' }, undefined)).toBeNull()
    expect(findReleaseMismatch(['a'], { a: '0.8.0' }, 'unknown')).toBeNull()
  })

  it('has nothing to compare for a frontend-only feature', () => {
    expect(findReleaseMismatch([], { a: '0.8.0' }, '0.8.1')).toBeNull()
  })
})
