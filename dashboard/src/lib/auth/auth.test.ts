/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { checkAuthStatus } from './auth'
import { AUTH_STORAGE_KEYS } from './auth.constants'
import { useAuthStore } from '../../stores/auth.store'

/**
 * In-memory Web Storage, installed before any import runs: the persisted Zustand
 * stores that `auth.ts` pulls in read their storage at module load.
 */
const { send, backing } = vi.hoisted(() => {
  const backing = new Map<string, string>()
  const storage = {
    getItem: (key: string) => backing.get(key) ?? null,
    setItem: (key: string, value: string) => void backing.set(key, value),
    removeItem: (key: string) => void backing.delete(key),
  }
  vi.stubGlobal('localStorage', storage)
  vi.stubGlobal('sessionStorage', storage)
  return { send: vi.fn(), backing }
})

vi.mock('./cognito-client', () => ({ getCognitoClient: () => ({ send }) }))
vi.mock('../config', () => ({ getCognitoClientId: () => 'test-client-id' }))

/** Unsigned JWT carrying only `exp`; the app decodes claims without verifying. */
function jwtExpiringAt(expSeconds: number): string {
  const payload = btoa(JSON.stringify({ exp: expSeconds })).replace(/=+$/, '')
  return `header.${payload}.signature`
}

const nowSeconds = Math.floor(Date.now() / 1000)
const VALID_TOKEN = jwtExpiringAt(nowSeconds + 3600)
const EXPIRED_TOKEN = jwtExpiringAt(nowSeconds - 3600)
const REFRESH_TOKEN = 'refresh-token'

function storeSession(accessToken: string, refreshToken: string | null): void {
  backing.set(AUTH_STORAGE_KEYS.ACCESS_TOKEN, accessToken)
  backing.set(AUTH_STORAGE_KEYS.ID_TOKEN, accessToken)
  if (refreshToken) {
    backing.set(AUTH_STORAGE_KEYS.REFRESH_TOKEN, refreshToken)
  }
}

function storedTokens(): Record<string, string | null> {
  return {
    access: backing.get(AUTH_STORAGE_KEYS.ACCESS_TOKEN) ?? null,
    id: backing.get(AUTH_STORAGE_KEYS.ID_TOKEN) ?? null,
    refresh: backing.get(AUTH_STORAGE_KEYS.REFRESH_TOKEN) ?? null,
  }
}

beforeEach(() => {
  backing.clear()
  send.mockReset()
  useAuthStore.getState().clearCredentials()
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('checkAuthStatus', () => {
  it('is false with no session and never calls Cognito', async () => {
    await expect(checkAuthStatus()).resolves.toBe(false)
    expect(send).not.toHaveBeenCalled()
  })

  it('trusts an unexpired access token without a network call', async () => {
    storeSession(VALID_TOKEN, REFRESH_TOKEN)

    await expect(checkAuthStatus()).resolves.toBe(true)
    expect(send).not.toHaveBeenCalled()
  })

  it('renews an expired access token and keeps the refresh token', async () => {
    storeSession(EXPIRED_TOKEN, REFRESH_TOKEN)
    send.mockResolvedValue({
      AuthenticationResult: { AccessToken: VALID_TOKEN, IdToken: VALID_TOKEN },
    })

    await expect(checkAuthStatus()).resolves.toBe(true)
    expect(storedTokens()).toEqual({
      access: VALID_TOKEN,
      id: VALID_TOKEN,
      refresh: REFRESH_TOKEN,
    })
  })

  it('discards the whole session when Cognito rejects the refresh token', async () => {
    storeSession(EXPIRED_TOKEN, REFRESH_TOKEN)
    useAuthStore.getState().setCredentials({
      accessKeyId: 'key',
      secretAccessKey: 'secret',
      sessionToken: 'session',
      expiration: nowSeconds + 3600,
    })
    send.mockRejectedValue(
      Object.assign(new Error('Refresh Token has expired'), {
        name: 'NotAuthorizedException',
      }),
    )

    await expect(checkAuthStatus()).resolves.toBe(false)
    expect(storedTokens()).toEqual({ access: null, id: null, refresh: null })
    expect(useAuthStore.getState().credentials).toBeNull()
  })

  it('discards the session when there is no refresh token to renew with', async () => {
    storeSession(EXPIRED_TOKEN, null)

    await expect(checkAuthStatus()).resolves.toBe(false)
    expect(send).not.toHaveBeenCalled()
    expect(storedTokens()).toEqual({ access: null, id: null, refresh: null })
  })

  it('keeps the session across a transient refresh failure', async () => {
    storeSession(EXPIRED_TOKEN, REFRESH_TOKEN)
    send.mockRejectedValue(new TypeError('Failed to fetch'))

    await expect(checkAuthStatus()).resolves.toBe(false)
    expect(storedTokens()).toEqual({
      access: EXPIRED_TOKEN,
      id: EXPIRED_TOKEN,
      refresh: REFRESH_TOKEN,
    })
  })
})
