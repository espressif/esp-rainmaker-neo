/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * CA and device certificate generation using Web Crypto API + ASN.1 DER encoding.
 * Produces X.509v3 certificates matching the admin CLI's output.
 * Ported from esp_rainmaker_dashboard/src/utils/bulkGenerate/certGen.js
 */

import {
  derSequence, derSet, derOid, derUtf8String, derPrintableString,
  derBitString, derExplicit, derBoolean, derOctetString, derInteger,
  derTime, ecdsaSigToDer, toPem,
  OID_COMMON_NAME, OID_COUNTRY, OID_STATE, OID_LOCALITY, OID_ORG, OID_ORG_UNIT,
  OID_EC_PUBLIC_KEY, OID_PRIME256V1, OID_ECDSA_WITH_SHA256, OID_BASIC_CONSTRAINTS,
} from './der-helpers'

const sigAlgDer = derSequence(derOid(OID_ECDSA_WITH_SHA256))

/** Build an X.500 Name from attribute pairs */
function buildName(attrs: [number[], string, boolean?][]): Uint8Array {
  const rdns = attrs.map(([oid, value, printable]) =>
    derSet(derSequence(derOid(oid), printable ? derPrintableString(value) : derUtf8String(value))),
  )
  return derSequence(...rdns)
}

/** Generate a random serial number (20 bytes, positive) */
function randomSerial(): Uint8Array {
  const bytes = new Uint8Array(20)
  crypto.getRandomValues(bytes)
  bytes[0] &= 0x7f // ensure positive
  if (bytes[0] === 0) {bytes[0] = 1}
  return bytes
}

/** Build a SubjectPublicKeyInfo DER from a raw EC public key */
function buildSpki(rawPubKey: Uint8Array): Uint8Array {
  return derSequence(
    derSequence(derOid(OID_EC_PUBLIC_KEY), derOid(OID_PRIME256V1)),
    derBitString(rawPubKey),
  )
}

/** Sign a TBS structure and wrap into the final Certificate DER */
async function signAndWrap(tbsDer: Uint8Array, signingKey: CryptoKey): Promise<Uint8Array> {
  const sigRaw = new Uint8Array(
    await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, signingKey, tbsDer.buffer as ArrayBuffer),
  )
  const sigDer = ecdsaSigToDer(sigRaw)
  return derSequence(tbsDer, sigAlgDer, derBitString(sigDer))
}

/**
 * Build a complete SEC1 ECPrivateKey DER from a CryptoKey (P-256).
 */
async function exportSec1Key(
  privateKey: CryptoKey,
  publicKey: CryptoKey,
): Promise<Uint8Array> {
  const jwk = await crypto.subtle.exportKey('jwk', privateKey)
  const rawPub = new Uint8Array(await crypto.subtle.exportKey('raw', publicKey))

  if (!jwk.d) {
    throw new Error('Private JWK is missing the d parameter')
  }
  const dBytes = new Uint8Array(
    atob(jwk.d.replace(/-/g, '+').replace(/_/g, '/'))
      .split('')
      .map((c) => c.charCodeAt(0)),
  )

  const version = derInteger(1)
  const privKeyOctet = derOctetString(dBytes)
  const curveParam = derExplicit(0, derOid(OID_PRIME256V1))
  const pubBitString = derBitString(rawPub)
  const pubKeyParam = derExplicit(1, pubBitString)

  return derSequence(version, privKeyOctet, curveParam, pubKeyParam)
}

export interface CaResult {
  caCertPem: string
  caKeyPem: string
  caKey: CryptoKey
  issuerDer: Uint8Array
}

/**
 * Generate a self-signed CA certificate.
 */
export async function generateCA(): Promise<CaResult> {
  const keyPair = await crypto.subtle.generateKey(
    { name: 'ECDSA', namedCurve: 'P-256' },
    true,
    ['sign'],
  )

  const rawPub = new Uint8Array(await crypto.subtle.exportKey('raw', keyPair.publicKey))

  const issuerDer = buildName([
    [OID_COMMON_NAME, 'com.rainmaker'],
    [OID_ORG, 'ESP'],
    [OID_ORG_UNIT, 'RM'],
    [OID_LOCALITY, 'PNE'],
    [OID_STATE, 'MH'],
    [OID_COUNTRY, 'IN', true],
  ])

  const now = new Date()
  const notBefore = new Date(now)
  notBefore.setDate(notBefore.getDate() - 1)
  const notAfter = new Date(now)
  notAfter.setFullYear(notAfter.getFullYear() + 100)

  const validity = derSequence(derTime(notBefore), derTime(notAfter))

  const extensions = derExplicit(
    3,
    derSequence(
      derSequence(
        derOid(OID_BASIC_CONSTRAINTS),
        derBoolean(true),
        derOctetString(derSequence(derBoolean(true))),
      ),
    ),
  )

  const tbsCertificate = derSequence(
    derExplicit(0, derInteger(2)),
    derInteger(randomSerial()),
    sigAlgDer,
    issuerDer,
    validity,
    issuerDer,
    buildSpki(rawPub),
    extensions,
  )

  const certDer = await signAndWrap(tbsCertificate, keyPair.privateKey)
  const caCertPem = toPem(certDer, 'CERTIFICATE')

  const sec1Der = await exportSec1Key(keyPair.privateKey, keyPair.publicKey)
  const caKeyPem = toPem(sec1Der, 'EC PRIVATE KEY')

  return { caCertPem, caKeyPem, caKey: keyPair.privateKey, issuerDer }
}

export interface DeviceCertResult {
  certPem: string
  keyPem: string
  keyPemPkcs8: string
}

/**
 * Generate a device certificate signed by the CA.
 */
export async function generateDeviceCert(
  caKey: CryptoKey,
  issuerDer: Uint8Array,
  nodeId: string,
): Promise<DeviceCertResult> {
  const keyPair = await crypto.subtle.generateKey(
    { name: 'ECDSA', namedCurve: 'P-256' },
    true,
    ['sign'],
  )

  const rawPub = new Uint8Array(await crypto.subtle.exportKey('raw', keyPair.publicKey))
  const subjectDer = buildName([[OID_COMMON_NAME, nodeId]])

  const now = new Date()
  const notBefore = new Date(now)
  notBefore.setDate(notBefore.getDate() - 1)
  const notAfter = new Date(now)
  notAfter.setFullYear(notAfter.getFullYear() + 20)

  const validity = derSequence(derTime(notBefore), derTime(notAfter))

  const extensions = derExplicit(
    3,
    derSequence(
      derSequence(
        derOid(OID_BASIC_CONSTRAINTS),
        derBoolean(true),
        derOctetString(derSequence()),
      ),
    ),
  )

  const tbsCertificate = derSequence(
    derExplicit(0, derInteger(2)),
    derInteger(randomSerial()),
    sigAlgDer,
    issuerDer,
    validity,
    subjectDer,
    buildSpki(rawPub),
    extensions,
  )

  const certDer = await signAndWrap(tbsCertificate, caKey)
  const certPem = toPem(certDer, 'CERTIFICATE')

  const sec1Der = await exportSec1Key(keyPair.privateKey, keyPair.publicKey)
  const keyPem = toPem(sec1Der, 'EC PRIVATE KEY')

  const pkcs8Der = new Uint8Array(await crypto.subtle.exportKey('pkcs8', keyPair.privateKey))
  const keyPemPkcs8 = toPem(pkcs8Der, 'PRIVATE KEY')

  return { certPem, keyPem, keyPemPkcs8 }
}
