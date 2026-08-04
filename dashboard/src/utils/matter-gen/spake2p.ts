/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * SPAKE2+ verifier generation for Matter commissioning.
 *
 * Mirrors esp-matter-mfg-tool `deps/spake2p.py::generate_verifier`:
 *   ws = PBKDF2-HMAC-SHA256(LE32(passcode), salt, iterations, 80 bytes)
 *   w0 = int_be(ws[0:40])  mod n
 *   w1 = int_be(ws[40:80]) mod n
 *   L  = w1 * G            (P-256 base point)
 *   verifier = w0 (32 bytes BE) || L (65-byte uncompressed point)   // 97 bytes
 *
 * The 40-byte (320-bit) halves reduced mod the curve order are the
 * spec-mandated mechanism to avoid modulo bias.
 */

import { p256 } from '@noble/curves/p256'
import { pbkdf2 } from '@noble/hashes/pbkdf2'
import { sha256 } from '@noble/hashes/sha2'

const CURVE_ORDER = p256.CURVE.n
const W_SIZE = 40 // NIST256p.baselen (32) + 8
const SCALAR_BYTES = 32

function bytesToBigIntBE(bytes: Uint8Array): bigint {
  let n = 0n
  for (const b of bytes) {n = (n << 8n) | BigInt(b)}
  return n
}

function bigIntToBytesBE(value: bigint, length: number): Uint8Array {
  const out = new Uint8Array(length)
  let v = value
  for (let i = length - 1; i >= 0; i--) {
    out[i] = Number(v & 0xffn)
    v >>= 8n
  }
  return out
}

export interface Spake2pVerifierResult {
  /** Raw 97-byte verifier (w0 || L). */
  verifier: Uint8Array
  /** Base64 of the verifier, as written to NVS / the registration payload. */
  verifierB64: string
}

export function generateSpake2pVerifier(
  passcode: number,
  salt: Uint8Array,
  iterations: number,
): Spake2pVerifierResult {
  // Passcode encoded as a 4-byte little-endian unsigned int.
  const pw = new Uint8Array(4)
  new DataView(pw.buffer).setUint32(0, passcode >>> 0, true)

  const ws = pbkdf2(sha256, pw, salt, { c: iterations, dkLen: W_SIZE * 2 })
  const w0 = bytesToBigIntBE(ws.subarray(0, W_SIZE)) % CURVE_ORDER
  const w1 = bytesToBigIntBE(ws.subarray(W_SIZE)) % CURVE_ORDER

  const L = p256.Point.BASE.multiply(w1).toBytes(false) // 0x04 || X || Y, 65 bytes
  const w0Bytes = bigIntToBytesBE(w0, SCALAR_BYTES)

  const verifier = new Uint8Array(SCALAR_BYTES + L.length)
  verifier.set(w0Bytes, 0)
  verifier.set(L, SCALAR_BYTES)

  return { verifier, verifierB64: bytesToBase64(verifier) }
}

function bytesToBase64(bytes: Uint8Array): string {
  let bin = ''
  for (const b of bytes) {bin += String.fromCharCode(b)}
  return btoa(bin)
}
