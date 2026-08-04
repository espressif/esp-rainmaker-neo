/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Matter Device Attestation certificate generation (PAA / PAI / DAC).
 *
 * Mirrors esp-matter-mfg-tool `cert_utils.build_certificate`: P-256 keys,
 * subject DN carrying the Matter VID/PID OID attributes, the required
 * BasicConstraints / KeyUsage / SubjectKeyIdentifier / AuthorityKeyIdentifier
 * extensions, signed with ECDSA-SHA256.
 *
 * Built on @noble/curves so it is synchronous and runs identically in Node
 * (tests) and the browser (dashboard integration).
 */

import { p256 } from '@noble/curves/p256'
import { sha256 } from '@noble/hashes/sha2'
import { sha1 } from '@noble/hashes/legacy'

import {
  derSequence,
  derSet,
  derOid,
  derUtf8String,
  derBitString,
  derExplicit,
  derBoolean,
  derOctetString,
  derInteger,
  derTime,
  toPem,
  OID_COMMON_NAME,
  OID_EC_PUBLIC_KEY,
  OID_PRIME256V1,
  OID_ECDSA_WITH_SHA256,
  OID_BASIC_CONSTRAINTS,
} from '../bulk-generate/der-helpers'
import { parseCertificate, parsePrivateScalar, pemToDer } from './asn1'

// Matter arc 1.3.6.1.4.1.37244.2.{1,2} (VID / PID DN attributes).
const OID_MATTER_VID = [0x2b, 0x06, 0x01, 0x04, 0x01, 0x82, 0xa2, 0x7c, 0x02, 0x01]
const OID_MATTER_PID = [0x2b, 0x06, 0x01, 0x04, 0x01, 0x82, 0xa2, 0x7c, 0x02, 0x02]
const OID_KEY_USAGE = [0x55, 0x1d, 0x0f]
const OID_SUBJECT_KEY_ID = [0x55, 0x1d, 0x0e]
const OID_AUTH_KEY_ID = [0x55, 0x1d, 0x23]

const SIG_ALG_DER = derSequence(derOid(OID_ECDSA_WITH_SHA256))

/** A signing handle: everything needed to issue a child certificate. */
export interface Issuer {
  privateScalar: Uint8Array
  subjectDer: Uint8Array
  publicKeyRaw: Uint8Array
}

export interface CertKeyPair extends Issuer {
  certPem: string
  certDer: Uint8Array
  keyPem: string
}

export interface DacResult extends CertKeyPair {
  /** DAC subject Common Name (a UUIDv4) — used as the cloud thing name. */
  commonName: string
}

const rdn = (oid: number[], value: string): Uint8Array =>
  derSet(derSequence(derOid(oid), derUtf8String(value)))

const hex4 = (n: number): string => (n & 0xffff).toString(16).toUpperCase().padStart(4, '0')

function subjectWithVidPid(commonName: string, vendorId: number, productId: number): Uint8Array {
  return derSequence(
    rdn(OID_COMMON_NAME, commonName),
    rdn(OID_MATTER_VID, hex4(vendorId)),
    rdn(OID_MATTER_PID, hex4(productId)),
  )
}

function randomSerial(): Uint8Array {
  const bytes = new Uint8Array(20)
  crypto.getRandomValues(bytes)
  bytes[0] &= 0x7f // positive
  if (bytes[0] === 0) {bytes[0] = 1}
  return bytes
}

function buildSpki(rawPub: Uint8Array): Uint8Array {
  return derSequence(
    derSequence(derOid(OID_EC_PUBLIC_KEY), derOid(OID_PRIME256V1)),
    derBitString(rawPub),
  )
}

function bitStringValue(unusedBits: number, ...valueBytes: number[]): Uint8Array {
  return new Uint8Array([0x03, valueBytes.length + 1, unusedBits, ...valueBytes])
}

function extBasicConstraints(isCa: boolean, pathLen?: number): Uint8Array {
  let inner: Uint8Array
  if (!isCa) {
    inner = derSequence() // cA defaults to FALSE
  } else if (pathLen === undefined) {
    inner = derSequence(derBoolean(true))
  } else {
    inner = derSequence(derBoolean(true), derInteger(pathLen))
  }
  return derSequence(derOid(OID_BASIC_CONSTRAINTS), derBoolean(true), derOctetString(inner))
}

function extKeyUsage(isCa: boolean): Uint8Array {
  // CA: digitalSignature + keyCertSign + crlSign (0x86, 1 unused bit).
  // Leaf (DAC): digitalSignature only (0x80, 7 unused bits).
  const ku = isCa ? bitStringValue(1, 0x86) : bitStringValue(7, 0x80)
  return derSequence(derOid(OID_KEY_USAGE), derBoolean(true), derOctetString(ku))
}

function extSubjectKeyId(rawPub: Uint8Array): Uint8Array {
  const keyId = sha1(rawPub) // RFC5280 method 1: SHA-1 of the public key
  return derSequence(derOid(OID_SUBJECT_KEY_ID), derOctetString(derOctetString(keyId)))
}

function extAuthorityKeyId(issuerRawPub: Uint8Array): Uint8Array {
  const keyId = sha1(issuerRawPub)
  const ctx0 = new Uint8Array([0x80, keyId.length, ...keyId]) // [0] IMPLICIT keyIdentifier
  return derSequence(derOid(OID_AUTH_KEY_ID), derOctetString(derSequence(ctx0)))
}

function signTbs(tbsDer: Uint8Array, signerScalar: Uint8Array): Uint8Array {
  const sig = p256.sign(sha256(tbsDer), signerScalar, { prehash: false })
  const sigDer = sig.toBytes('der')
  return derSequence(tbsDer, SIG_ALG_DER, derBitString(sigDer))
}

interface BuildCertArgs {
  subjectDer: Uint8Array
  subjectPubRaw: Uint8Array
  issuer: Issuer
  isCa: boolean
  pathLen?: number
  validityYears: number
}

function buildCert(a: BuildCertArgs): Uint8Array {
  const notBefore = new Date()
  const notAfter = new Date(notBefore)
  notAfter.setFullYear(notAfter.getFullYear() + a.validityYears)

  const extensions = derExplicit(
    3,
    derSequence(
      extBasicConstraints(a.isCa, a.pathLen),
      extKeyUsage(a.isCa),
      extSubjectKeyId(a.subjectPubRaw),
      extAuthorityKeyId(a.issuer.publicKeyRaw),
    ),
  )

  const tbs = derSequence(
    derExplicit(0, derInteger(2)), // version v3
    derInteger(randomSerial()),
    SIG_ALG_DER,
    a.issuer.subjectDer,
    derSequence(derTime(notBefore), derTime(notAfter)),
    a.subjectDer,
    buildSpki(a.subjectPubRaw),
    extensions,
  )

  return signTbs(tbs, a.issuer.privateScalar)
}

function sec1KeyPem(scalar: Uint8Array, rawPub: Uint8Array): string {
  const sec1 = derSequence(
    derInteger(1),
    derOctetString(scalar),
    derExplicit(0, derOid(OID_PRIME256V1)),
    derExplicit(1, derBitString(rawPub)),
  )
  return toPem(sec1, 'EC PRIVATE KEY')
}

function newKeyPair(): { scalar: Uint8Array; pub: Uint8Array } {
  const scalar = p256.utils.randomSecretKey()
  const pub = p256.getPublicKey(scalar, false) // uncompressed 0x04 || X || Y
  return { scalar, pub }
}

function pack(certDer: Uint8Array, scalar: Uint8Array, pub: Uint8Array, subjectDer: Uint8Array): CertKeyPair {
  return {
    certDer,
    certPem: toPem(certDer, 'CERTIFICATE'),
    keyPem: sec1KeyPem(scalar, pub),
    privateScalar: scalar,
    publicKeyRaw: pub,
    subjectDer,
  }
}

/** Generate a self-signed PAA (test root). VID/PID are intentionally absent. */
export function generatePaa(commonName = 'Matter Test PAA'): CertKeyPair {
  const { scalar, pub } = newKeyPair()
  const subjectDer = derSequence(rdn(OID_COMMON_NAME, commonName))
  const self: Issuer = { privateScalar: scalar, subjectDer, publicKeyRaw: pub }
  const certDer = buildCert({
    subjectDer,
    subjectPubRaw: pub,
    issuer: self,
    isCa: true,
    pathLen: 1,
    validityYears: 100,
  })
  return pack(certDer, scalar, pub, subjectDer)
}

export interface PaiOptions {
  vendorId: number
  productId: number
  commonName?: string
}

/** Generate a PAI signed by the given PAA. */
export function generatePai(paa: Issuer, opts: PaiOptions): CertKeyPair {
  const { scalar, pub } = newKeyPair()
  const subjectDer = subjectWithVidPid(opts.commonName ?? 'ESP32 PAI 00', opts.vendorId, opts.productId)
  const certDer = buildCert({
    subjectDer,
    subjectPubRaw: pub,
    issuer: paa,
    isCa: true,
    pathLen: 0,
    validityYears: 100,
  })
  return pack(certDer, scalar, pub, subjectDer)
}

export interface DacOptions {
  vendorId: number
  productId: number
  /** DAC CN; defaults to a fresh UUIDv4 (the cloud thing name). */
  commonName?: string
}

/** Generate a DAC signed by the given PAI. */
export function generateDac(pai: Issuer, opts: DacOptions): DacResult {
  const commonName = opts.commonName ?? crypto.randomUUID()
  const { scalar, pub } = newKeyPair()
  const subjectDer = subjectWithVidPid(commonName, opts.vendorId, opts.productId)
  const certDer = buildCert({
    subjectDer,
    subjectPubRaw: pub,
    issuer: pai,
    isCa: false,
    validityYears: 100,
  })
  return { ...pack(certDer, scalar, pub, subjectDer), commonName }
}

/**
 * Build an Issuer handle from an existing PAI certificate + private key PEM
 * (e.g. the official CHIP test PAI), so generated DACs chain to a PAA that
 * standard test commissioners already trust.
 */
export function loadIssuer(certPem: string, keyPem: string): Issuer {
  const { subjectDer, publicKeyRaw } = parseCertificate(pemToDer(certPem))
  return { privateScalar: parsePrivateScalar(keyPem), subjectDer, publicKeyRaw }
}
