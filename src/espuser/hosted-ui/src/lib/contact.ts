/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** One field on screen, two on the wire: sign-up takes exactly one of `email` or `phone_number`. */
export interface Contact {
  email?: string
  phone_number?: string
}

/** A dialable number: digits with the spaces, brackets, dots and dashes people type between them. */
const DIALABLE = /^\+?[0-9][0-9\s().-]*$/

/** normalizeUsername is the address as the server stores it: trimmed, and a phone number without its punctuation: ' +91 (98) 765-4321' -> '+91987654321'. */
export function normalizeUsername(value: string): string {
  const trimmed = value.trim()
  return !trimmed.includes('@') && DIALABLE.test(trimmed) ? trimmed.replace(/[\s().-]/g, '') : trimmed
}

/** An address with an @ is an email, a dialable number is a phone, and anything else goes as email for the server to reject by name. */
export function contactFields(value: string): Contact {
  const username = normalizeUsername(value)
  return /^\+?[0-9]+$/.test(username) ? { phone_number: username } : { email: username }
}
