/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { Brand, PageData, Provider } from '../page-data.ts'

export interface Option {
  /** The provider's registry name. */
  id: string
  label: string
  brand?: Brand
  /** Where a redirect goes; absent on a pool answered on this page. */
  href?: string
}

interface SignInOptions {
  redirects: Option[]
  pools: Option[]
  otp: boolean
}

type Screen =
  | { kind: 'error'; error: NonNullable<PageData['error']> }
  | { kind: 'logout'; logout: NonNullable<PageData['logout']> }
  | { kind: 'consent'; consent: NonNullable<PageData['consent']> }
  | { kind: 'signed-out' }
  | { kind: 'sign-in' }

/** screenFor reads the page's purpose: an error the request cannot get past, then a question the server is asking, then a finished sign-out, else signing in. */
export function screenFor(page: PageData): Screen {
  if (page.error) return { kind: 'error', error: page.error }
  if (page.logout) return { kind: 'logout', logout: page.logout }
  if (page.consent) return { kind: 'consent', consent: page.consent }
  if (page.signed_out) return { kind: 'signed-out' }
  return { kind: 'sign-in' }
}

const UNNAMED_REDIRECT = 'Continue'

/** labelFor never shows the registry key ("cognito"), which means nothing to a person; a display name equal to it counts as unset. */
function labelFor(p: Provider): string {
  const named = p.display_name?.trim()
  if (named && named.toLowerCase() !== p.name.toLowerCase()) return named
  return p.method === 'password' ? 'Email and password' : UNNAMED_REDIRECT
}

/** signInOptions splits providers in Go's order: a password provider is answered here, anything else is a redirect. */
export function signInOptions(page: PageData): SignInOptions {
  const options: SignInOptions = { redirects: [], pools: [], otp: page.otp_enabled }
  for (const p of page.providers) {
    const option: Option = { id: p.name, label: labelFor(p), brand: p.brand }
    if (p.method === 'password') options.pools.push(option)
    // Relative to /user/login so the stage prefix carries over; the name is escaped since an operator wrote it.
    else options.redirects.push({ ...option, href: `login/federation/start?provider=${encodeURIComponent(p.name)}` })
  }
  return options
}

/** The name each brand's own button and the redirect screen use. */
export const BRAND_NAMES: Record<Brand, string> = { google: 'Google', apple: 'Apple', github: 'GitHub' }

/** destinationName is what the page calls the provider a redirect goes to: Google's option -> 'Google', 'RainMaker Staging' -> 'RainMaker Staging', one with no display name -> 'your sign-in provider'. */
export function destinationName(option: Option): string {
  if (option.brand) return BRAND_NAMES[option.brand]
  return option.label === UNNAMED_REDIRECT ? 'your sign-in provider' : option.label
}

/** initialPool is the pool the client hinted with idp, else the first: ['rainmaker', 'insights'] with idp 'insights' -> 'insights'. */
export function initialPool(pools: Option[], hint?: string): string | null {
  return pools.find(p => p.id === hint)?.id ?? pools[0]?.id ?? null
}

/** clientLabel is the client's name as the page shows it, or null for none: ' Home ' -> 'Home', '  ' -> null. */
export function clientLabel(clientName?: string): string | null {
  return clientName?.trim() || null
}
