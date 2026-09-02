/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  getLastLoginEmail,
  migrateLegacyAuthStorage,
  storeLastLoginEmail,
} from './auth.storage'
import { AUTH_STORAGE_KEYS } from './auth.constants'

/** Minimal in-memory localStorage, since the node test environment has none. */
function installLocalStorage() {
  const backing = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => backing.get(key) ?? null,
    setItem: (key: string, value: string) => void backing.set(key, value),
    removeItem: (key: string) => void backing.delete(key),
  })
  return backing
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('last-login email storage', () => {
  it('round-trips the stored address', () => {
    installLocalStorage()

    storeLastLoginEmail('admin@example.com')
    expect(getLastLoginEmail()).toBe('admin@example.com')
  })

  it('returns null when nothing is stored', () => {
    installLocalStorage()

    expect(getLastLoginEmail()).toBeNull()
  })

  it('falls back to null when localStorage is unusable', () => {
    // No stub installed: the node environment has no `localStorage`, so every
    // access throws — exactly the private-browsing / blocked-storage case.
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)

    expect(getLastLoginEmail()).toBeNull()
    expect(warn).toHaveBeenCalled()
  })

  it('swallows write failures instead of throwing', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)

    expect(() => storeLastLoginEmail('admin@example.com')).not.toThrow()
    expect(warn).toHaveBeenCalled()
  })
})

describe('legacy auth storage migration', () => {
  it('moves pre-prefix values to the prefixed keys and drops the legacy entries', () => {
    const backing = installLocalStorage()
    backing.set('access_token', 'at')
    backing.set('id_token', 'it')
    backing.set('refresh_token', 'rt')
    backing.set('keep_signed_in', 'true')

    migrateLegacyAuthStorage()

    expect(backing.get(AUTH_STORAGE_KEYS.ACCESS_TOKEN)).toBe('at')
    expect(backing.get(AUTH_STORAGE_KEYS.ID_TOKEN)).toBe('it')
    expect(backing.get(AUTH_STORAGE_KEYS.REFRESH_TOKEN)).toBe('rt')
    expect(backing.get(AUTH_STORAGE_KEYS.KEEP_SIGNED_IN)).toBe('true')
    expect(backing.has('access_token')).toBe(false)
    expect(backing.has('id_token')).toBe(false)
    expect(backing.has('refresh_token')).toBe(false)
    expect(backing.has('keep_signed_in')).toBe(false)
  })

  it('never overwrites a value already stored under the new key', () => {
    const backing = installLocalStorage()
    backing.set('access_token', 'stale')
    backing.set(AUTH_STORAGE_KEYS.ACCESS_TOKEN, 'fresh')

    migrateLegacyAuthStorage()

    expect(backing.get(AUTH_STORAGE_KEYS.ACCESS_TOKEN)).toBe('fresh')
    expect(backing.has('access_token')).toBe(false)
  })

  it('is a no-op on clean storage', () => {
    const backing = installLocalStorage()

    expect(() => migrateLegacyAuthStorage()).not.toThrow()
    expect(backing.size).toBe(0)
  })

  it('swallows storage failures instead of blocking startup', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)

    expect(() => migrateLegacyAuthStorage()).not.toThrow()
    expect(warn).toHaveBeenCalled()
  })
})
