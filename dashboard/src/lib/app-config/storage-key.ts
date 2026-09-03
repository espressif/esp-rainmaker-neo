/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

// Imported directly (like the barrel does) rather than via the barrel, so the
// barrel can re-export this module without a cycle.
import appConfig from '../../../app.config'

/**
 * The one place the app-wide storage namespace is formatted. Every *persistent*
 * storage key — auth constants, persisted Zustand store names — is built here, so
 * the prefix and separator cannot drift between definition sites.
 *
 * Deliberately not baked into `safe-storage` itself: that wrapper must stay a
 * transparent guard over Web Storage. The legacy-key migration reads the old
 * unprefixed names through it, the PKCE/OAuth hand-off keys are unprefixed on
 * purpose, and Zustand's `persist` bypasses the wrapper entirely — an
 * auto-prefixing storage layer would need escape hatches for all three.
 */
export function appStorageKey(name: string): string {
  return `${appConfig.storagePrefix}-${name}`
}
