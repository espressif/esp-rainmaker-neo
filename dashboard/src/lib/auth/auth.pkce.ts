/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * PKCE (Proof Key for Code Exchange) utilities
 * Used for secure OAuth 2.0 authorization code flow
 */

/**
 * Base64 URL encode an ArrayBuffer
 * Converts binary data to URL-safe base64 string
 */
function base64UrlEncode(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let str = ''
  for (let i = 0; i < bytes.byteLength; i++) {
    str += String.fromCharCode(bytes[i])
  }
  return btoa(str)
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/g, '')
}

/**
 * Generate SHA-256 hash and encode as base64url
 */
async function sha256Base64Url(input: string): Promise<string> {
  const encoder = new TextEncoder()
  const data = encoder.encode(input)
  const digest = await crypto.subtle.digest('SHA-256', data)
  return base64UrlEncode(digest)
}

/**
 * Generate a random PKCE code verifier
 * Returns a cryptographically random 43-128 character string
 */
export function generateCodeVerifier(): string {
  const random = new Uint8Array(32)
  crypto.getRandomValues(random)
  return base64UrlEncode(random.buffer)
}

/**
 * Generate PKCE code challenge from verifier
 * Uses S256 method (SHA-256 hash of verifier)
 */
export async function generateCodeChallenge(verifier: string): Promise<string> {
  return sha256Base64Url(verifier)
}

/**
 * Generate a random state parameter for CSRF protection
 */
export function generateState(): string {
  return crypto.randomUUID()
}

/**
 * Generate complete PKCE parameters for OAuth flow
 */
export async function generatePkceParams(): Promise<{
  verifier: string
  challenge: string
  state: string
}> {
  const verifier = generateCodeVerifier()
  const challenge = await generateCodeChallenge(verifier)
  const state = generateState()

  return { verifier, challenge, state }
}

