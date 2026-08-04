/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { p256 } from '@noble/curves/p256'
import { sha256 } from '@noble/hashes/sha2'

import { getChipTestCredentials } from './chip-test-credentials'
import { generateDac, loadIssuer } from './matter-cert'
import { readChildren, readTlv } from './asn1'

describe('bundled CHIP test credentials', () => {
  it('provides PAI + CD for FFF2/8001 and signs a verifiable DAC', () => {
    const creds = getChipTestCredentials(0xfff2, 0x8001)
    expect(creds).toBeDefined()
    if (!creds) {
      return
    }
    expect(creds.paiCertPem).toContain('-----BEGIN CERTIFICATE-----')
    expect(creds.paiKeyPem).toContain('-----BEGIN EC PRIVATE KEY-----')
    expect(creds.cdDer.length).toBeGreaterThan(0)

    const pai = loadIssuer(creds.paiCertPem, creds.paiKeyPem)
    expect(pai.publicKeyRaw.length).toBe(65)

    // A DAC signed by the loaded test PAI verifies against the PAI public key.
    const dac = generateDac(pai, { vendorId: 0xfff2, productId: 0x8001 })
    const cert = readTlv(dac.certDer, 0)
    const [tbsNode, , sigNode] = readChildren(dac.certDer, cert)
    const tbs = dac.certDer.slice(tbsNode.start, tbsNode.end)
    const sig = dac.certDer.slice(sigNode.contentStart + 1, sigNode.end)
    expect(p256.verify(sig, sha256(tbs), pai.publicKeyRaw, { prehash: false, format: 'der' })).toBe(
      true,
    )
  })

  it('returns undefined for an unbundled VID/PID', () => {
    expect(getChipTestCredentials(0x1234, 0x5678)).toBeUndefined()
  })
})
