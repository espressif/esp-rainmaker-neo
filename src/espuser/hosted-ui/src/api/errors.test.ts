/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { describeError, endsSignIn, terminalHeading } from './errors.ts'

describe('describeError', () => {
  it('leaves invalid_grant, which no sign-in page endpoint answers, to the generic error (negative)', () => {
    assert.deepEqual(describeError({ error: 'invalid_grant' }), describeError(null))
  })

  it('never shows the server description, which is written for an operator (negative)', () => {
    const shown = describeError({ error: 'invalid_credentials', error_description: 'InitiateAuth failed for pool us-east-1_abc' })
    assert.doesNotMatch(shown.message, /pool|InitiateAuth/)
  })

  it('still offers a retry for a code it has never seen (negative)', () => {
    assert.deepEqual(describeError({ error: 'something_we_never_shipped' }), describeError(null))
    assert.equal(describeError(null).action, 'retry')
  })

  it("shows the account endpoints' own sentence, which has no OAuth code", () => {
    assert.deepEqual(describeError({ message: 'Password did not conform with policy' }),
      { message: 'Password did not conform with policy', action: 'retry', severity: 'error' })
  })

  it('keeps an account that needs a new password on the form, saying who can help, since no screen here can set one', () => {
    const shown = describeError({ error: 'new_password_required' })
    assert.match(shown.message, /support/i)
    assert.ok(!endsSignIn(shown))
  })

  it('sends an account an admin reset to forgot password, and keeps the form open for it', () => {
    const shown = describeError({ error: 'password_reset_required' })
    assert.match(shown.message, /Forgot password/)
    assert.ok(!endsSignIn(shown))
  })

  it('says plainly when the app does not allow this way of signing in, or the account cannot be signed in, instead of the generic error', () => {
    for (const code of ['provider_not_allowed', 'access_denied']) {
      const shown = describeError({ error: code })
      assert.notDeepEqual(shown, describeError(null), code)
      assert.equal(shown.action, 'retry', code)
    }
    assert.equal(describeError({ error: 'provider_not_allowed' }).message, 'This app does not allow signing in this way.')
  })

  it('tells a wrong code, an expired code and a failed send apart', () => {
    const shown = ['invalid_code', 'expired_code', 'delivery_failed'].map(code => describeError({ error: code }).message)
    assert.equal(new Set(shown).size, 3)
  })
})

describe('endsSignIn', () => {
  it('ends the screen for a sign-in that is over, offering Start again only for one that was not replaced', () => {
    const cases: [string, string, boolean][] = [
      ['expired_flow', 'restart', true],
      ['code_locked', 'restart', true],
      ['flow_replaced', 'replaced', true],
      ['invalid_code', 'retry', false],
      ['expired_code', 'retry', false],
      ['delivery_failed', 'retry', false],
      ['too_many_requests', 'retry', false],
    ]
    for (const [code, action, ends] of cases) {
      const shown = describeError({ error: code })
      assert.deepEqual([shown.action, endsSignIn(shown)], [action, ends], code)
    }
  })
})

describe('terminalHeading', () => {
  it("heads an integrator's sentence in words for the person, and keeps one already written for the person (negative)", () => {
    const blocked = 'This account cannot be signed in. Please contact support.'
    const cases: [string | undefined, string, string][] = [
      ['invalid_request', 'response_type must be code.', 'This sign-in can’t continue'],
      ['invalid_client', 'Unknown client.', 'This sign-in can’t continue'],
      ['server_error', 'Internal server error.', 'Something went wrong on our side'],
      ['access_denied', blocked, blocked],
      [undefined, 'This sign-in took too long.', 'This sign-in took too long.'],
    ]
    for (const [code, message, heading] of cases) assert.equal(terminalHeading(code, message), heading, code)
  })
})
