/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { base38Decode, base38Encode } from './base38'

describe('base38', () => {
  // Least-significant Base38 digit is emitted first; chunk sizes 1/2/3 bytes
  // produce 2/4/5 chars respectively.
  it('encodes known byte sequences', () => {
    expect(base38Encode(new Uint8Array([]))).toBe('')
    expect(base38Encode(new Uint8Array([1]))).toBe('10')
    expect(base38Encode(new Uint8Array([1, 2]))).toBe('JD00')
    expect(base38Encode(new Uint8Array([0xff]))).toBe('R6')
    expect(base38Encode(new Uint8Array([0xff, 0xff]))).toBe('NE71')
    expect(base38Encode(new Uint8Array([0xff, 0xff, 0xff]))).toBe('PLS18')
  })

  it('round-trips arbitrary bytes', () => {
    for (const len of [1, 2, 3, 4, 5, 11, 16]) {
      const bytes = new Uint8Array(len)
      for (let i = 0; i < len; i++) {bytes[i] = (i * 37 + 13) & 0xff}
      expect(base38Decode(base38Encode(bytes))).toEqual(bytes)
    }
  })
})
