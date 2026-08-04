/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Minimal DER reader — just enough to extract the issuer subject DN, public
 * key, and private scalar from an existing PAI certificate / key (so the
 * official CHIP test PAI can be plugged into DAC generation).
 *
 * This is intentionally tiny; it is not a general-purpose ASN.1 library.
 */

export interface DerNode {
  tag: number
  /** Offset of the tag byte (start of the whole TLV). */
  start: number
  /** Offset of the content (after tag + length). */
  contentStart: number
  /** Offset one past the content (== end of the whole TLV). */
  end: number
}

/** Read a single TLV starting at `offset`. */
export function readTlv(buf: Uint8Array, offset: number): DerNode {
  const tag = buf[offset]
  let i = offset + 1
  let len = buf[i++]
  if (len & 0x80) {
    const numBytes = len & 0x7f
    len = 0
    for (let k = 0; k < numBytes; k++) {len = (len << 8) | buf[i++]}
  }
  return { tag, start: offset, contentStart: i, end: i + len }
}

/** Read all immediate children of a constructed TLV node. */
export function readChildren(buf: Uint8Array, node: DerNode): DerNode[] {
  const out: DerNode[] = []
  let off = node.contentStart
  while (off < node.end) {
    const child = readTlv(buf, off)
    out.push(child)
    off = child.end
  }
  return out
}

export function pemToDer(pem: string): Uint8Array {
  const b64 = pem
    .replace(/-----BEGIN [^-]+-----/, '')
    .replace(/-----END [^-]+-----/, '')
    .replace(/\s+/g, '')
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) {out[i] = bin.charCodeAt(i)}
  return out
}

export interface ParsedIssuer {
  /** Raw DER of the subject Name SEQUENCE (used as the DAC issuer). */
  subjectDer: Uint8Array
  /** Uncompressed public key point (0x04 || X || Y, 65 bytes). */
  publicKeyRaw: Uint8Array
}

/**
 * Extract the subject DN bytes and SubjectPublicKey from an X.509 certificate.
 */
export function parseCertificate(certDer: Uint8Array): ParsedIssuer {
  const cert = readTlv(certDer, 0)
  const [tbs] = readChildren(certDer, cert)
  const tbsChildren = readChildren(certDer, tbs)

  // [0] EXPLICIT version is optional; when present it's the first child (tag 0xA0).
  const base = tbsChildren[0].tag === 0xa0 ? 1 : 0
  // children: [version?] serial, sigAlg, issuer, validity, subject, spki, ...
  const subject = tbsChildren[base + 4]
  const spki = tbsChildren[base + 5]

  const subjectDer = certDer.slice(subject.start, subject.end)

  const spkiChildren = readChildren(certDer, spki)
  const bitString = spkiChildren[1] // BIT STRING
  // First content byte of a BIT STRING is the unused-bits count; skip it.
  const publicKeyRaw = certDer.slice(bitString.contentStart + 1, bitString.end)

  return { subjectDer, publicKeyRaw }
}

/**
 * Extract the raw 32-byte private scalar from a SEC1 ("EC PRIVATE KEY") or
 * PKCS#8 ("PRIVATE KEY") PEM.
 */
export function parsePrivateScalar(keyPem: string): Uint8Array {
  const der = pemToDer(keyPem)
  const root = readTlv(der, 0)
  const children = readChildren(der, root)

  // SEC1 ECPrivateKey:   SEQ { INTEGER(1), OCTET STRING(d), [0] params, [1] pub }
  // PKCS#8 PrivateKeyInfo: SEQ { INTEGER(0), AlgId, OCTET STRING( SEC1 inner ) }
  const versionVal = der[children[0].contentStart]

  if (versionVal === 1) {
    const octet = children[1]
    return der.slice(octet.contentStart, octet.end)
  }

  const inner = children[2]
  const innerSeq = readTlv(der, inner.contentStart)
  const octet = readChildren(der, innerSeq)[1]
  return der.slice(octet.contentStart, octet.end)
}
