/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Post-login redirect plumbing.
 *
 * The destination travels as a `redirect` search param rather than storage, so a
 * dead session can hand it over across the hard navigation `logout()` performs.
 *
 * Two shapes, one policy: `logout()` takes a plain href string, while the router's
 * `redirect()` wants structured search (its `href` option is for absolute URLs and
 * infers a full document reload, which a route guard does not want). Both go through
 * {@link redirectTarget} so what counts as a legitimate destination is decided once.
 */

import { isInternalPath } from '../navigation/internal-path'

/** Search param carrying the path to restore after a successful sign-in. */
export const LOGIN_REDIRECT_PARAM = 'redirect'

/**
 * Vets a post-login destination, returning `undefined` when it should be dropped:
 * anything that isn't an internal path, and `/login` itself — bouncing the login
 * page back to the login page is a loop, not a destination.
 */
function redirectTarget(from: string): string | undefined {
  if (!isInternalPath(from) || from.startsWith('/login')) {
    return undefined
  }

  return from
}

/**
 * Builds the `/login` href that returns the user to `from` once they sign in again.
 * For `logout()` and anything else navigating by raw URL.
 */
export function loginHrefWithRedirect(from: string): string {
  const target = redirectTarget(from)
  if (!target) {
    return '/login'
  }

  return `/login?${LOGIN_REDIRECT_PARAM}=${encodeURIComponent(target)}`
}

/**
 * The same destination as router search params, for `redirect({ to: '/login', search })`.
 * `undefined` when there is nothing worth restoring, which the router reads as no search.
 */
export function loginRedirectSearch(
  from: string,
): { redirect: string } | undefined {
  const target = redirectTarget(from)

  return target ? { [LOGIN_REDIRECT_PARAM]: target } : undefined
}
