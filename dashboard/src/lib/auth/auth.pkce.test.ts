/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { webcrypto } from 'node:crypto'
import { beforeAll, describe, expect, it } from 'vitest'
import {
  generateCodeChallenge,
  generateCodeVerifier,
  generatePkceParams,
  generateState,
} from './auth.pkce'

/** RFC 7636 appendix B: the canonical verifier/challenge pair every S256 implementation must match. */
const RFC_7636_VERIFIER = 'dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk'
const RFC_7636_CHALLENGE = 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM'

const BASE64URL_ONLY = /^[A-Za-z0-9\-_]+$/

/**
 * These are browser globals; the node test environment does not expose them, so stand them up
 * from the equivalent node builtins rather than moving the unit under test off the Web Crypto API.
 */
beforeAll(() => {
  if (!globalThis.crypto) {
    Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true })
  }
  if (typeof globalThis.btoa !== 'function') {
    globalThis.btoa = (input: string) => Buffer.from(input, 'binary').toString('base64')
  }
})

describe('generateCodeVerifier', () => {
  it('is base64url with no padding or non-URL-safe characters', () => {
    const verifier = generateCodeVerifier()
    expect(verifier).toMatch(BASE64URL_ONLY)
    expect(verifier).not.toMatch(/[+/=]/)
  })

  it('stays inside the 43-128 character range RFC 7636 allows', () => {
    const verifier = generateCodeVerifier()
    expect(verifier.length).toBeGreaterThanOrEqual(43)
    expect(verifier.length).toBeLessThanOrEqual(128)
  })

  it('does not repeat across calls', () => {
    const verifiers = new Set(Array.from({ length: 32 }, () => generateCodeVerifier()))
    expect(verifiers.size).toBe(32)
  })
})

describe('generateCodeChallenge', () => {
  it('derives the RFC 7636 reference challenge', async () => {
    await expect(generateCodeChallenge(RFC_7636_VERIFIER)).resolves.toBe(RFC_7636_CHALLENGE)
  })

  it('emits base64url with no padding or non-URL-safe characters', async () => {
    const challenge = await generateCodeChallenge(generateCodeVerifier())
    expect(challenge).toMatch(BASE64URL_ONLY)
    expect(challenge).not.toMatch(/[+/=]/)
    // 32 raw SHA-256 bytes, base64url-encoded without padding.
    expect(challenge).toHaveLength(43)
  })
})

describe('generateState', () => {
  it('does not repeat across calls', () => {
    const states = new Set(Array.from({ length: 32 }, () => generateState()))
    expect(states.size).toBe(32)
  })
})

describe('generatePkceParams', () => {
  it('returns a challenge derived from the verifier it hands back', async () => {
    const { verifier, challenge, state } = await generatePkceParams()
    await expect(generateCodeChallenge(verifier)).resolves.toBe(challenge)
    expect(state.length).toBeGreaterThan(0)
  })
})
