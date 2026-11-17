/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { isKnownExtensionSlot } from './slots'

describe('isKnownExtensionSlot', () => {
  it('accepts a slot listed by its exact name', () => {
    expect(isKnownExtensionSlot('account-settings.preferences.extra')).toBe(true)
    expect(isKnownExtensionSlot('nodes.register.sections')).toBe(true)
  })

  it('accepts a generated slot through its pattern', () => {
    expect(isKnownExtensionSlot('nodes.register.tags.extra')).toBe(true)
    expect(isKnownExtensionSlot('nodes.register.basic-details.extra')).toBe(true)
  })

  it('rejects a slot this dashboard does not have', () => {
    expect(isKnownExtensionSlot('ota.jobs.header-actions')).toBe(false)
    expect(isKnownExtensionSlot('account-settings.preferences.extraa')).toBe(false)
  })

  it('does not let a pattern segment span a dot or be empty', () => {
    expect(isKnownExtensionSlot('nodes.register.a.b.extra')).toBe(false)
    expect(isKnownExtensionSlot('nodes.register..extra')).toBe(false)
  })
})
