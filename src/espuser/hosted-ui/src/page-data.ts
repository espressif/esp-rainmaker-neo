/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** The brands whose own sign-in button the page draws; mirrors Go's buttonBrands. */
export type Brand = 'google' | 'apple' | 'github'

/** Mirrors Go's providerData. */
export interface Provider {
  name: string
  display_name?: string
  brand?: Brand
  /** "password" is answered by this page's own form; "redirect" sends the person to the provider's own page. */
  method: 'password' | 'redirect'
}

/** Mirrors Go's pageData. */
export interface PageData {
  /** logo_uri is an inline image the registry validated; absent draws a generic app icon. */
  client: { name?: string; logo_uri?: string }
  providers: Provider[]
  otp_enabled: boolean
  /** Sent back by every call that finishes the sign-in; its cookie is HttpOnly, so the server injects it here. */
  flow_id?: string
  login_hint?: string
  idp?: string
  restart_url?: string
  consent?: { account?: string; scopes: string[] }
  /** csrf_token goes back with the person's answer to the sign-out question. */
  logout?: { account?: string; csrf_token: string }
  /** Where a sign-out with nowhere validated to return lands. */
  signed_out?: boolean
  /** Each is an https URL the server checked; an absent one is not drawn. */
  links?: { support?: string; privacy?: string; terms?: string }
  error?: { code?: string; message: string }
}

const EMPTY: PageData = { client: {}, providers: [], otp_enabled: false }

/** Reads the data Go injected; a missing or broken block yields an empty description, never a blank screen. */
export function readPageData(doc: Document = document): PageData {
  const el = doc.getElementById('esp-page-data')
  if (!el?.textContent) return EMPTY
  try {
    const parsed = JSON.parse(el.textContent) as Partial<PageData>
    // Go writes a nil slice as null, so every list the screens map over is defaulted here.
    return {
      ...parsed,
      client: parsed.client ?? {},
      providers: parsed.providers ?? [],
      otp_enabled: parsed.otp_enabled ?? false,
      consent: parsed.consent && { ...parsed.consent, scopes: parsed.consent.scopes ?? [] },
    }
  } catch {
    return EMPTY
  }
}
