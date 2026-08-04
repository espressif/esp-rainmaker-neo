/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { p256 } from '@noble/curves/p256'
import { sha256 } from '@noble/hashes/sha2'

import { generateDac, generatePaa, generatePai, loadIssuer } from './matter-cert'
import { pemToDer, readChildren, readTlv } from './asn1'

const VID = 0xfff2
const PID = 0x8001

/** Pull (tbsDer, signatureDer) out of an X.509 certificate. */
function splitCert(certDer: Uint8Array): { tbs: Uint8Array; sig: Uint8Array } {
  const cert = readTlv(certDer, 0)
  const [tbsNode, , sigNode] = readChildren(certDer, cert)
  const tbs = certDer.slice(tbsNode.start, tbsNode.end)
  // signature BIT STRING: skip the leading unused-bits byte.
  const sig = certDer.slice(sigNode.contentStart + 1, sigNode.end)
  return { tbs, sig }
}

function verifyChain(childCertDer: Uint8Array, issuerPubRaw: Uint8Array): boolean {
  const { tbs, sig } = splitCert(childCertDer)
  return p256.verify(sig, sha256(tbs), issuerPubRaw, { prehash: false, format: 'der' })
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

describe('matter-cert', () => {
  it('builds a PAA→PAI→DAC chain that verifies', () => {
    const paa = generatePaa()
    const pai = generatePai(paa, { vendorId: VID, productId: PID })
    const dac = generateDac(pai, { vendorId: VID, productId: PID })

    // Each cert is signed by its issuer's key.
    expect(verifyChain(paa.certDer, paa.publicKeyRaw)).toBe(true) // self-signed
    expect(verifyChain(pai.certDer, paa.publicKeyRaw)).toBe(true)
    expect(verifyChain(dac.certDer, pai.publicKeyRaw)).toBe(true)
  })

  it('uses a UUIDv4 DAC common name and emits valid PEM', () => {
    const paa = generatePaa()
    const pai = generatePai(paa, { vendorId: VID, productId: PID })
    const dac = generateDac(pai, { vendorId: VID, productId: PID })

    expect(dac.commonName).toMatch(UUID_RE)
    expect(dac.certPem).toContain('-----BEGIN CERTIFICATE-----')
    expect(dac.keyPem).toContain('-----BEGIN EC PRIVATE KEY-----')
    expect(dac.publicKeyRaw.length).toBe(65)
    expect(dac.publicKeyRaw[0]).toBe(0x04)
  })

  it('encodes Matter VID/PID OID attributes in the DAC subject', () => {
    const paa = generatePaa()
    const pai = generatePai(paa, { vendorId: VID, productId: PID })
    const dac = generateDac(pai, { vendorId: 0xfff1, productId: 0x8000 })
    // OID 1.3.6.1.4.1.37244.2.1 DER bytes, then UTF8String "FFF1".
    const vidOid = Uint8Array.from([0x2b, 0x06, 0x01, 0x04, 0x01, 0x82, 0xa2, 0x7c, 0x02, 0x01])
    const hay = Array.from(dac.certDer).join(',')
    expect(hay.includes(Array.from(vidOid).join(','))).toBe(true)
    expect(dac.certPem).toBeTruthy()
  })

  it('round-trips a generated PAI through loadIssuer (parser)', () => {
    const paa = generatePaa()
    const pai = generatePai(paa, { vendorId: VID, productId: PID })

    const loaded = loadIssuer(pai.certPem, pai.keyPem)
    expect(Array.from(loaded.subjectDer)).toEqual(Array.from(pai.subjectDer))
    expect(Array.from(loaded.publicKeyRaw)).toEqual(Array.from(pai.publicKeyRaw))
    expect(Array.from(loaded.privateScalar)).toEqual(Array.from(pai.privateScalar))

    // A DAC signed via the loaded handle still verifies against the PAI key.
    const dac = generateDac(loaded, { vendorId: VID, productId: PID })
    const { tbs, sig } = splitCert(dac.certDer)
    expect(p256.verify(sig, sha256(tbs), pai.publicKeyRaw, { prehash: false, format: 'der' })).toBe(
      true,
    )
  })
})

// silence unused import warning when pemToDer isn't referenced directly
void pemToDer
