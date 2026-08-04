/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { generateSpake2pVerifier } from './spake2p'

describe('spake2p verifier', () => {
  // Canonical connectedhomeip vector: passcode 20202021,
  // salt = ASCII "SPAKE2P Key Salt", iteration count 1000.
  it('matches the canonical Matter verifier', () => {
    const salt = new TextEncoder().encode('SPAKE2P Key Salt')
    const { verifier, verifierB64 } = generateSpake2pVerifier(20202021, salt, 1000)

    expect(verifier.length).toBe(97)
    expect(verifier[32]).toBe(0x04) // L is an uncompressed point
    expect(verifierB64).toBe(
      'uWFwqugDNGiEck/po7KHwwMwwqZgN10XuyBajPGuyzUEV/iree4lOrao5GuwnlQ65CJzbeUB49s31EH+NEkg0JVI5MGCQGMMT/SRPFNRODm3wH/MBiehuFc6FJ/NH6Rmzw==',
    )
  })

  it('is deterministic for fixed inputs', () => {
    const salt = new Uint8Array(32).fill(7)
    const a = generateSpake2pVerifier(12345678 + 1, salt, 1000).verifierB64
    const b = generateSpake2pVerifier(12345679, salt, 1000).verifierB64
    expect(a).toBe(b)
  })
})
