/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { describeScopes, stayConnected } from './scopes.ts'

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

  it('leaves offline access out of the list, which the screen shows apart as how long access lasts', () => {
    assert.deepEqual(describeScopes(['openid', 'offline_access']), [{ label: 'Identity', detail: 'Your Espressif account ID' }])
  })

  it('says offline access in the app\'s name and how to undo it', () => {
    assert.deepEqual(stayConnected('Alexa'), {
      label: 'Stay connected', detail: "Alexa keeps working while you're signed out. Disconnect anytime.",
    })
  })

  it('names a scope it has no words for rather than hiding it (negative)', () => {
    assert.deepEqual(describeScopes(['devices:write']), [
      { label: 'Identity', detail: 'Your Espressif account ID' },
      { label: 'devices:write' },
    ])
  })
})
