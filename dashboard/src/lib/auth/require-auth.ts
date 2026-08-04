/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Route guard for protected routes
 * Use in beforeLoad of any route requiring authentication
 */

import { redirect } from '@tanstack/react-router'
import { checkAuthStatus } from './auth'
import { loginRedirectSearch } from './login-redirect'

interface RequireAuthContext {
  location: {
    href: string
    pathname: string
  }
}

/**
 * beforeLoad guard that ensures user is authenticated
 * Redirects to /login if not authenticated, carrying the requested location so a
 * deep link survives the sign-in instead of dumping the user on the default page.
 */
export async function requireAuth({ location }: RequireAuthContext) {
  const isAuthenticated = await checkAuthStatus()

  if (!isAuthenticated) {
    throw redirect({
      to: '/login',
      search: loginRedirectSearch(location.href),
    })
  }

  return { isAuthenticated: true }
}
