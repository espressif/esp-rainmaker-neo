/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * ASN.1 DER encoding helpers for X.509 certificate construction.
 * Ported from esp_rainmaker_dashboard/src/utils/bulkGenerate/derHelpers.js
 */

export const concatBytes = (...arrays: (Uint8Array | number[])[]): Uint8Array => {
  const flat = arrays.map((a) => (a instanceof Uint8Array ? a : new Uint8Array(a)))
  const len = flat.reduce((s, a) => s + a.length, 0)
  const result = new Uint8Array(len)
  let offset = 0
  for (const a of flat) {
    result.set(a, offset)
    offset += a.length
  }
  return result
}

export const derLength = (len: number): number[] => {
  if (len < 0x80) {return [len]}
  if (len < 0x100) {return [0x81, len]}
  return [0x82, (len >> 8) & 0xff, len & 0xff]
}

export const derSequence = (...items: Uint8Array[]): Uint8Array => {
  const body = concatBytes(...items)
  return new Uint8Array([0x30, ...derLength(body.length), ...body])
}

export const derSet = (...items: Uint8Array[]): Uint8Array => {
  const body = concatBytes(...items)
  return new Uint8Array([0x31, ...derLength(body.length), ...body])
}

export const derOid = (oidBytes: number[]): Uint8Array =>
  new Uint8Array([0x06, oidBytes.length, ...oidBytes])

export const derUtf8String = (str: string): Uint8Array => {
  const encoded = new TextEncoder().encode(str)
  return new Uint8Array([0x0c, ...derLength(encoded.length), ...encoded])
}

export const derPrintableString = (str: string): Uint8Array => {
  const encoded = new TextEncoder().encode(str)
  return new Uint8Array([0x13, ...derLength(encoded.length), ...encoded])
}

export const derBitString = (data: Uint8Array): Uint8Array =>
  new Uint8Array([0x03, ...derLength(data.length + 1), 0x00, ...data])

export const derExplicit = (tag: number, data: Uint8Array): Uint8Array =>
  new Uint8Array([0xa0 | tag, ...derLength(data.length), ...data])

export const derBoolean = (val: boolean): Uint8Array =>
  new Uint8Array([0x01, 0x01, val ? 0xff : 0x00])

export const derOctetString = (data: Uint8Array): Uint8Array =>
  new Uint8Array([0x04, ...derLength(data.length), ...data])

export const derInteger = (value: number | bigint | Uint8Array): Uint8Array => {
  let bytes: number[]
  if (typeof value === 'number') {
    if (value === 0) {return new Uint8Array([0x02, 0x01, 0x00])}
    bytes = []
    let v = value
    while (v > 0) {
      bytes.unshift(v & 0xff)
      v >>= 8
    }
  } else if (value instanceof Uint8Array) {
    bytes = Array.from(value)
  } else {
    // BigInt
    bytes = []
    let v = value
    while (v > 0n) {
      bytes.unshift(Number(v & 0xffn))
      v >>= 8n
    }
    if (bytes.length === 0) {bytes = [0]}
  }
  // Ensure positive: pad with 0x00 if high bit set
  if (bytes[0] & 0x80) {bytes.unshift(0x00)}
  return new Uint8Array([0x02, ...derLength(bytes.length), ...bytes])
}

export const derUTCTime = (date: Date): Uint8Array => {
  const yy = String(date.getUTCFullYear() % 100).padStart(2, '0')
  const mm = String(date.getUTCMonth() + 1).padStart(2, '0')
  const dd = String(date.getUTCDate()).padStart(2, '0')
  const hh = String(date.getUTCHours()).padStart(2, '0')
  const mi = String(date.getUTCMinutes()).padStart(2, '0')
  const ss = String(date.getUTCSeconds()).padStart(2, '0')
  const str = `${yy}${mm}${dd}${hh}${mi}${ss}Z`
  const encoded = new TextEncoder().encode(str)
  return new Uint8Array([0x17, encoded.length, ...encoded])
}

export const derGeneralizedTime = (date: Date): Uint8Array => {
  const yyyy = String(date.getUTCFullYear()).padStart(4, '0')
  const mm = String(date.getUTCMonth() + 1).padStart(2, '0')
  const dd = String(date.getUTCDate()).padStart(2, '0')
  const hh = String(date.getUTCHours()).padStart(2, '0')
  const mi = String(date.getUTCMinutes()).padStart(2, '0')
  const ss = String(date.getUTCSeconds()).padStart(2, '0')
  const str = `${yyyy}${mm}${dd}${hh}${mi}${ss}Z`
  const encoded = new TextEncoder().encode(str)
  return new Uint8Array([0x18, encoded.length, ...encoded])
}

/** Encode a Date as UTCTime (year < 2050) or GeneralizedTime (year >= 2050) */
export const derTime = (date: Date): Uint8Array =>
  date.getUTCFullYear() < 2050 ? derUTCTime(date) : derGeneralizedTime(date)

/** Convert IEEE P1363 ECDSA signature (r||s, 64 bytes) to DER */
export const ecdsaSigToDer = (sigRaw: Uint8Array): Uint8Array => {
  const r = sigRaw.slice(0, 32)
  const s = sigRaw.slice(32, 64)
  const derInt = (bytes: Uint8Array): Uint8Array => {
    let start = 0
    while (start < bytes.length - 1 && bytes[start] === 0) {start++}
    const trimmed = bytes.slice(start)
    const pad = trimmed[0] & 0x80 ? [0x00] : []
    return new Uint8Array([0x02, pad.length + trimmed.length, ...pad, ...trimmed])
  }
  return derSequence(derInt(r), derInt(s))
}

/** Encode data as PEM with the given label */
export const toPem = (der: Uint8Array, label: string): string => {
  const b64 = btoa(String.fromCharCode(...der))
  const chunks = b64.match(/.{1,64}/g)
  if (!chunks) {
    throw new Error('Failed to encode PEM body')
  }
  const lines = chunks.join('\n')
  return `-----BEGIN ${label}-----\n${lines}\n-----END ${label}-----\n`
}

// Well-known OIDs
export const OID_COMMON_NAME = [0x55, 0x04, 0x03]
export const OID_COUNTRY = [0x55, 0x04, 0x06]
export const OID_STATE = [0x55, 0x04, 0x08]
export const OID_LOCALITY = [0x55, 0x04, 0x07]
export const OID_ORG = [0x55, 0x04, 0x0a]
export const OID_ORG_UNIT = [0x55, 0x04, 0x0b]
export const OID_EC_PUBLIC_KEY = [0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01]
export const OID_PRIME256V1 = [0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07]
export const OID_ECDSA_WITH_SHA256 = [0x2a, 0x86, 0x48, 0xce, 0x3d, 0x04, 0x03, 0x02]
export const OID_BASIC_CONSTRAINTS = [0x55, 0x1d, 0x13]
