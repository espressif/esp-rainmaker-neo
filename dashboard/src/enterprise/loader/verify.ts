/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

function bytesToBase64(bytes: Uint8Array): string {
  let bin = ''
  for (const byte of bytes) {
    bin += String.fromCharCode(byte)
  }
  return btoa(bin)
}

/** Compute the `sha384-<base64>` integrity string for the given bytes. */
export async function sha384Integrity(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-384', bytes.buffer as ArrayBuffer)
  return `sha384-${bytesToBase64(new Uint8Array(digest))}`
}
