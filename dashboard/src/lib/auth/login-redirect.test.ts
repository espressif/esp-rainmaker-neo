/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { isInternalPath } from '../navigation/internal-path'
import {
  LOGIN_REDIRECT_PARAM,
  loginHrefWithRedirect,
  loginRedirectSearch,
} from './login-redirect'

describe('isInternalPath', () => {
  it('accepts same-origin relative paths', () => {
    expect(isInternalPath('/home')).toBe(true)
    expect(isInternalPath('/home/node-management/nodes?tab=all')).toBe(true)
  })

  it('rejects anything that could leave the origin', () => {
    // Protocol-relative: a browser resolves this to https://evil.com.
    expect(isInternalPath('//evil.com')).toBe(false)
    expect(isInternalPath('https://evil.com')).toBe(false)
    expect(isInternalPath('javascript:alert(1)')).toBe(false)
    expect(isInternalPath('home')).toBe(false)
  })

  it('rejects non-strings and empty input', () => {
    expect(isInternalPath('')).toBe(false)
    expect(isInternalPath(undefined)).toBe(false)
    expect(isInternalPath(null)).toBe(false)
    expect(isInternalPath(['/home'])).toBe(false)
  })
})

describe('loginHrefWithRedirect', () => {
  it('encodes the destination into the redirect param', () => {
    expect(loginHrefWithRedirect('/home/node-management/nodes')).toBe(
      `/login?${LOGIN_REDIRECT_PARAM}=%2Fhome%2Fnode-management%2Fnodes`
    )
  })

  it('encodes a query string so it survives the round trip', () => {
    const href = loginHrefWithRedirect('/home/ota/jobs?status=IN_PROGRESS')
    expect(href).toBe(
      `/login?${LOGIN_REDIRECT_PARAM}=%2Fhome%2Fota%2Fjobs%3Fstatus%3DIN_PROGRESS`
    )
    // The param must decode back to exactly what was handed in.
    expect(
      decodeURIComponent(new URLSearchParams(href.split('?')[1]).get(LOGIN_REDIRECT_PARAM) ?? '')
    ).toBe('/home/ota/jobs?status=IN_PROGRESS')
  })

  it('falls back to a bare /login for off-origin destinations', () => {
    expect(loginHrefWithRedirect('//evil.com')).toBe('/login')
    expect(loginHrefWithRedirect('https://evil.com')).toBe('/login')
    expect(loginHrefWithRedirect('')).toBe('/login')
  })

  it('never points the login page back at itself', () => {
    expect(loginHrefWithRedirect('/login')).toBe('/login')
    expect(loginHrefWithRedirect('/login?reset=success')).toBe('/login')
  })
})

describe('loginRedirectSearch', () => {
  it('returns the destination unencoded — the router does its own encoding', () => {
    expect(loginRedirectSearch('/home/ota/jobs?status=IN_PROGRESS')).toEqual({
      [LOGIN_REDIRECT_PARAM]: '/home/ota/jobs?status=IN_PROGRESS',
    })
  })

  it('returns undefined when there is nothing worth restoring', () => {
    expect(loginRedirectSearch('//evil.com')).toBeUndefined()
    expect(loginRedirectSearch('https://evil.com')).toBeUndefined()
    expect(loginRedirectSearch('')).toBeUndefined()
    expect(loginRedirectSearch('/login')).toBeUndefined()
  })

  it('agrees with loginHrefWithRedirect on what counts as a destination', () => {
    const cases = [
      '/home',
      '/home/node-management/nodes',
      '//evil.com',
      'https://evil.com',
      '/login',
      '',
    ]

    for (const from of cases) {
      const accepted = loginHrefWithRedirect(from) !== '/login'
      expect(loginRedirectSearch(from) !== undefined).toBe(accepted)
    }
  })
})
