/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Safe wrappers around the Web Storage API for imperative single-value access.
 *
 * Every call is guarded: blocked storage (private browsing, enterprise policy,
 * quota) and non-browser environments (unit tests) degrade to `null`/`false`
 * with a console warning instead of throwing. Object-shaped *reactive* state
 * does not belong here — persisted Zustand stores handle their own storage via
 * the `persist` middleware.
 *
 * Deliberately dependency- and alias-free so the alias-less `vitest.config.ts`
 * can load anything that imports it.
 */

interface SafeStorage {
  get(key: string): string | null
  /** @returns whether the write actually happened. */
  set(key: string, value: string): boolean
  /** @returns whether the removal call went through. */
  remove(key: string): boolean
}

type StorageKind = 'localStorage' | 'sessionStorage'

/**
 * Resolved lazily inside each guarded call — merely touching the storage object
 * can throw where storage is blocked. Looked up on `globalThis`, not `window`,
 * so a node test environment can provide a stub.
 */
function resolveStorage(kind: StorageKind): Storage {
  const storage = (globalThis as Partial<Record<StorageKind, Storage>>)[kind]
  if (!storage) {
    throw new Error(`${kind} is unavailable in this environment`)
  }
  return storage
}

function makeSafeStorage(kind: StorageKind): SafeStorage {
  return {
    get(key) {
      try {
        return resolveStorage(kind).getItem(key)
      } catch (error) {
        console.warn(`[Storage] Failed to read from ${kind}: ${key}`, error)
        return null
      }
    },
    set(key, value) {
      try {
        resolveStorage(kind).setItem(key, value)
        return true
      } catch (error) {
        console.warn(`[Storage] Failed to write to ${kind}: ${key}`, error)
        return false
      }
    },
    remove(key) {
      try {
        resolveStorage(kind).removeItem(key)
        return true
      } catch (error) {
        console.warn(`[Storage] Failed to remove from ${kind}: ${key}`, error)
        return false
      }
    },
  }
}

export const safeLocalStorage: SafeStorage = makeSafeStorage('localStorage')
export const safeSessionStorage: SafeStorage = makeSafeStorage('sessionStorage')
