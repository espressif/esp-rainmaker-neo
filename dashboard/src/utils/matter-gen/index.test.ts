/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { p256 } from '@noble/curves/p256'
import { sha256 } from '@noble/hashes/sha2'

import { generateMatterDevice } from './index'
import { generateSpake2pVerifier } from './spake2p'
import { generateQrPayload, generateManualPairingCode } from './setup-payload'
import { parseCertificate, pemToDer, readChildren, readTlv } from './asn1'
import { getChipTestCredentials } from './chip-test-credentials'

describe('generateMatterDevice', () => {
  it('produces a self-consistent device bundle', () => {
    const d = generateMatterDevice({ vendorId: 0xfff2, productId: 0x8001 })

    // Identity
    expect(d.thingName).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    )
    expect(d.dac.commonName).toBe(d.thingName)

    // Commissioning parameters within spec ranges
    expect(d.discriminator).toBeGreaterThanOrEqual(0)
    expect(d.discriminator).toBeLessThan(4096)
    expect(d.passcode).toBeGreaterThanOrEqual(1)
    expect(d.passcode).toBeLessThanOrEqual(99999998)
    expect(d.salt.length).toBe(32)
    expect(d.verifier.length).toBe(97)

    // Verifier matches a fresh computation over the same inputs
    expect(generateSpake2pVerifier(d.passcode, d.salt, d.iterationCount).verifierB64).toBe(
      d.verifierB64,
    )

    // QR / manual code match the recomputed payload
    const setup = {
      passcode: d.passcode,
      discriminator: d.discriminator,
      vendorId: d.vendorId,
      productId: d.productId,
    }
    expect(d.qrPayload).toBe(generateQrPayload(setup))
    expect(d.qrUrl.endsWith(d.qrPayload)).toBe(true)
    expect(d.manualPairingCode).toBe(generateManualPairingCode(setup))
  })

  it('signs the DAC with the bundled CHIP test PAI (not self-signed)', () => {
    const d = generateMatterDevice({ vendorId: 0xfff2, productId: 0x8001 })

    // DAC carries the bundled test PAI cert as its chain parent.
    const creds = getChipTestCredentials(0xfff2, 0x8001)
    expect(creds).toBeDefined()
    if (!creds) {
      return
    }
    expect(d.pai.certPem).toBe(creds.paiCertPem)

    const { publicKeyRaw: paiPub } = parseCertificate(pemToDer(creds.paiCertPem))
    const cert = readTlv(d.dac.certDer, 0)
    const [tbsNode, , sigNode] = readChildren(d.dac.certDer, cert)
    const tbs = d.dac.certDer.slice(tbsNode.start, tbsNode.end)
    const sig = d.dac.certDer.slice(sigNode.contentStart + 1, sigNode.end)

    // Verifies against the PAI key, NOT the DAC's own key (i.e. not self-signed).
    expect(p256.verify(sig, sha256(tbs), paiPub, { prehash: false, format: 'der' })).toBe(true)
    expect(p256.verify(sig, sha256(tbs), d.dac.publicKeyRaw, { prehash: false, format: 'der' })).toBe(
      false,
    )
  })

  it('embeds the bundled CHIP test CD as cert-dclrn', () => {
    const d = generateMatterDevice({ vendorId: 0xfff2, productId: 0x8001 })
    const cd = d.nvsRows.find((r) => r.key === 'cert-dclrn')
    expect(cd).toBeDefined()
    expect(cd?.value).toBeInstanceOf(Uint8Array)
  })

  it('throws for a VID/PID with no bundled PAI when none is supplied', () => {
    expect(() => generateMatterDevice({ vendorId: 0x1234, productId: 0x5678 })).toThrow(/No PAI/)
  })

  it('emits the expected chip-factory NVS rows', () => {
    const d = generateMatterDevice({ mqttHost: 'example-ats.iot.us-east-1.amazonaws.com' })
    const keys = d.nvsRows.map((r) => r.key)

    expect(d.nvsRows[0]).toMatchObject({ key: 'chip-factory', type: 'namespace' })
    for (const k of [
      'discriminator',
      'iteration-count',
      'salt',
      'verifier',
      'dac-cert',
      'dac-key',
      'dac-pub-key',
      'pai-cert',
      'vendor-id',
      'product-id',
      'hardware-ver',
      'rd-id-uid',
    ]) {
      expect(keys).toContain(k)
    }

    // u32 typing + base64 string storage for salt/verifier
    const disc = d.nvsRows.find((r) => r.key === 'discriminator')
    expect(disc).toBeDefined()
    expect(disc).toMatchObject({ type: 'data', encoding: 'u32' })
    expect(disc?.value).toBe(d.discriminator)
    expect(d.nvsRows.find((r) => r.key === 'salt')?.value).toBe(d.saltB64)
    expect(d.nvsRows.find((r) => r.key === 'verifier')?.value).toBe(d.verifierB64)

    // merged rmaker_creds namespace
    expect(keys).toContain('rmaker_creds')
    expect(d.nvsRows.find((r) => r.key === 'client_id')?.value).toBe(d.thingName)
  })

  it('honours fixed passcode/discriminator and matches the canonical onboarding code', () => {
    // Manual code is VID/PID-independent in Standard flow; use the bundled
    // FFF2/8001 PAI so generation succeeds.
    const d = generateMatterDevice({
      passcode: 20202021,
      discriminator: 3840,
      vendorId: 0xfff2,
      productId: 0x8001,
    })
    expect(d.manualPairingCode).toBe('34970112332')
  })
})
