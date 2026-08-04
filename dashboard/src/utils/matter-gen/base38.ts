/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Matter Base38 encoding (used by the onboarding QR payload).
 *
 * Mirrors connectedhomeip / esp-matter-mfg-tool `Base38.encode`:
 * input bytes are processed in chunks of up to 3 (little-endian within a
 * chunk), and each chunk emits a fixed number of Base38 characters,
 * least-significant digit first:
 *   1 byte  -> 2 chars
 *   2 bytes -> 4 chars
 *   3 bytes -> 5 chars
 */

const CODES = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ-.'
const RADIX = 38
// Indexed by (bytesInChunk - 1).
const CHARS_PER_CHUNK = [2, 4, 5]

export function base38Encode(bytes: Uint8Array): string {
  let out = ''
  for (let i = 0; i < bytes.length; i += 3) {
    const n = Math.min(3, bytes.length - i)
    // Up to 3 bytes => at most 24 bits, safe in a JS number.
    let value = 0
    for (let j = 0; j < n; j++) {value += bytes[i + j] * 2 ** (8 * j)}
    let charsNeeded = CHARS_PER_CHUNK[n - 1]
    while (charsNeeded-- > 0) {
      out += CODES[value % RADIX]
      value = Math.floor(value / RADIX)
    }
  }
  return out
}

export function base38Decode(str: string): Uint8Array {
  // Inverse of base38Encode — primarily here so tests can round-trip.
  const bytes: number[] = []
  for (let i = 0; i < str.length; i += 5) {
    const charsInChunk = Math.min(5, str.length - i)
    const bytesInChunk = CHARS_PER_CHUNK.indexOf(charsInChunk) + 1
    if (bytesInChunk === 0) {throw new Error(`invalid base38 chunk length: ${charsInChunk}`)}
    let value = 0
    for (let j = charsInChunk - 1; j >= 0; j--) {
      const idx = CODES.indexOf(str[i + j])
      if (idx < 0) {throw new Error(`invalid base38 char: ${str[i + j]}`)}
      value = value * RADIX + idx
    }
    for (let j = 0; j < bytesInChunk; j++) {bytes.push((value >> (8 * j)) & 0xff)}
  }
  return new Uint8Array(bytes)
}
