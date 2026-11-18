/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { describeScopes } from './scopes.ts'

describe('describeScopes', () => {
  it('names each grant and says what it gives the app, once each', () => {
    assert.deepEqual(describeScopes(['openid', 'email', 'profile', 'email']), [
      { label: 'Identity', detail: 'Your Espressif account ID' },
      { label: 'Email', detail: 'Your email address' },
      { label: 'Profile', detail: 'Your name, language and profile picture' },
    ])
  })

  it('always says the app learns who you are, even when it asked for nothing else', () => {
    assert.deepEqual(describeScopes([]), [{ label: 'Identity', detail: 'Your Espressif account ID' }])
  })

  it('names a scope it has no words for rather than hiding it (negative)', () => {
    assert.deepEqual(describeScopes(['devices:write']), [
      { label: 'Identity', detail: 'Your Espressif account ID' },
      { label: 'devices:write' },
    ])
  })
})
