/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { verhoeffCheckDigit } from './verhoeff'

describe('verhoeff', () => {
  it('matches known check digits', () => {
    // Classic Verhoeff examples.
    expect(verhoeffCheckDigit('236')).toBe('3')
    expect(verhoeffCheckDigit('12345')).toBe('1')
    expect(verhoeffCheckDigit('142857')).toBe('0')
  })

  it('produces the Matter canonical manual-code check digit', () => {
    // disc 3840 / passcode 20202021 -> manual payload "3497011233" + "2".
    expect(verhoeffCheckDigit('3497011233')).toBe('2')
  })
})
