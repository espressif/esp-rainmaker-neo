/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * WebCrypto verification for the enterprise delivery chain: an ECDSA P-256 detached JWS over the
 * manifest bytes (signed by Espressif's KMS key at publish time), and sha384 integrity hashes
 * over each bundle. No bytes run unless both checks pass.
 */

function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) {
    bytes[i] = bin.charCodeAt(i)
  }
  return bytes
}

function base64UrlToBytes(b64url: string): Uint8Array {
  const b64 = b64url.replace(/-/g, '+').replace(/_/g, '/')
  return base64ToBytes(b64 + '='.repeat((4 - (b64.length % 4)) % 4))
}

function bytesToBase64(bytes: Uint8Array): string {
  let bin = ''
  for (const byte of bytes) {
    bin += String.fromCharCode(byte)
  }
  return btoa(bin)
}

function bytesToBase64Url(bytes: Uint8Array): string {
  return bytesToBase64(bytes).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

/**
 * Verify a detached compact JWS (`protectedHeader..signature`, ES256) over the given payload
 * bytes. The signing input is `b64url(header) + '.' + b64url(payload)` per RFC 7515 — the
 * payload travels as the separate manifest.json object, not inside the JWS.
 */
export async function verifyDetachedJws(
  payload: Uint8Array,
  jws: string,
  publicKeySpkiB64: string,
): Promise<boolean> {
  const parts = jws.trim().split('.')
  if (parts.length !== 3 || parts[1] !== '') {
    return false
  }
  const [protectedHeader, , signatureB64] = parts
  try {
    const key = await crypto.subtle.importKey(
      'spki',
      base64ToBytes(publicKeySpkiB64).buffer as ArrayBuffer,
      { name: 'ECDSA', namedCurve: 'P-256' },
      false,
      ['verify'],
    )
    const signingInput = new TextEncoder().encode(
      `${protectedHeader}.${bytesToBase64Url(payload)}`,
    )
    return await crypto.subtle.verify(
      { name: 'ECDSA', hash: 'SHA-256' },
      key,
      base64UrlToBytes(signatureB64).buffer as ArrayBuffer,
      signingInput,
    )
  } catch {
    return false
  }
}

/** Compute the `sha384-<base64>` integrity string for the given bytes. */
export async function sha384Integrity(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-384', bytes.buffer as ArrayBuffer)
  return `sha384-${bytesToBase64(new Uint8Array(digest))}`
}
