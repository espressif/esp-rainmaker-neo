/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { contactFields, normalizeUsername } from './contact.ts'

describe('contactFields', () => {
  it('routes an address to email', () => {
    assert.deepEqual(contactFields('someone@example.com'), { email: 'someone@example.com' })
  })

  it('routes a dialable number to phone_number, without its punctuation', () => {
    assert.deepEqual(contactFields('+91 (98) 765-4321'), { phone_number: '+91987654321' })
  })

  it('always names exactly one field, which is what the server demands (negative)', () => {
    for (const value of ['someone@example.com', '+919876543210', 'not-an-address', ' ']) {
      assert.equal(Object.keys(contactFields(value)).length, 1)
    }
  })
})

describe('normalizeUsername', () => {
  it('trims an address and strips the punctuation people type in a phone number', () => {
    const cases: [string, string][] = [
      ['  someone@example.com ', 'someone@example.com'],
      [' +91 (98) 765-4321 ', '+91987654321'],
      ['+1.415.555.0100', '+14155550100'],
    ]
    for (const [typed, sent] of cases) assert.equal(normalizeUsername(typed), sent, typed)
  })

  it('leaves the dashes and dots of an email or a plain username alone (negative)', () => {
    for (const value of ['first.last-name@my-domain.example', 'not-a-number', 'user (work)']) {
      assert.equal(normalizeUsername(value), value)
    }
  })

  it('is what contactFields sends, so sign-in and sign-up name the same account', () => {
    for (const value of [' +91 (98) 765-4321', 'someone@example.com ']) {
      assert.equal(Object.values(contactFields(value))[0], normalizeUsername(value))
    }
  })
})
