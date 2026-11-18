/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { afterEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { account, api } from './client.ts'
import { describeError } from './errors.ts'

const realFetch = globalThis.fetch
afterEach(() => {
  globalThis.fetch = realFetch
  Reflect.deleteProperty(globalThis, 'window')
})

/** Answers every call with `status` and `body`, and returns the bodies the page sent. */
function answer(status: number, body: string): Record<string, unknown>[] {
  const sent: Record<string, unknown>[] = []
  globalThis.fetch = async (_url, init) => {
    sent.push(JSON.parse(String(init?.body)))
    return new Response(body, { status })
  }
  return sent
}

describe('what the page sends', () => {
  it('drops the spaces a paste or a phone keyboard leaves around an address', async () => {
    const sent = answer(401, '{"error":"invalid_credentials"}')
    await api.signIn('  someone@example.com ', 'pw', 'cognito')
    await api.sendCode(' someone@example.com', 'flow-1')
    await account.startRecovery('someone@example.com  ', 'cognito')
    await account.confirmRecovery(' someone@example.com ', '123456', 'pw', 'cognito')
    assert.deepEqual(sent.map(body => body.username), Array(4).fill('someone@example.com'))
  })

  it('sends a phone number without the spaces, brackets and dashes it was typed with', async () => {
    const sent = answer(401, '{"error":"invalid_credentials"}')
    await api.signIn(' +91 (98) 765-4321', 'pw', 'cognito')
    await api.sendCode('+91 98765 43210', 'flow-1')
    await account.startRecovery('+91-98765-43210', 'cognito')
    await account.confirmRecovery('+91 98765-43210 ', '123456', 'pw', 'cognito')
    assert.deepEqual(sent.map(body => body.username), ['+91987654321', '+919876543210', '+919876543210', '+919876543210'])
  })

  it('never trims a password, whose spaces are part of it (negative)', async () => {
    const sent = answer(401, '{"error":"invalid_credentials"}')
    await api.signIn('someone@example.com', ' pass word ', 'cognito')
    await account.confirmRecovery('someone@example.com', '123456', ' new pass ')
    assert.deepEqual([sent[0]?.password, sent[1]?.new_password], [' pass word ', ' new pass '])
  })

  it('names the sign-in it was drawn for on every call that finishes one, so the server can refuse a replaced page', async () => {
    const sent = answer(400, '{"error":"flow_replaced"}')
    await api.signIn('someone@example.com', 'pw', 'cognito', 'flow-1')
    await api.sendCode('someone@example.com', 'flow-1')
    await api.verifyCode('123456', 'flow-1')
    await api.answerConsent('allow', 'flow-1')
    assert.deepEqual(sent.map(body => body.flow_id), Array(4).fill('flow-1'))
  })
})

describe('where the page sends it', () => {
  // The page runs at /user/login, /user/login/consent and /user/logout, so each call is resolved the way a browser does, under the stage prefix of the default API host.
  async function landed(page: string, call: () => Promise<unknown>): Promise<string> {
    const urls: string[] = []
    globalThis.fetch = async (url) => {
      urls.push(String(url))
      return new Response('{}', { status: 400 })
    }
    await call()
    return new URL(urls[0] ?? '', `https://api.example.com/prod${page}`).pathname
  }

  it("signs in with a password on the page's own route, never the native token API", async () => {
    assert.equal(await landed('/user/login', () => api.signIn('someone@example.com', 'pw', 'cognito')), '/prod/user/login/password')
  })

  it('reaches the account routes from the sign-in page', async () => {
    assert.equal(await landed('/user/login', () => api.sendCode('someone@example.com')), '/prod/v1/auth/otp/initiate')
    assert.equal(await landed('/user/login', () => account.signUp('someone@example.com', 'pw')), '/prod/v1/user/auth/signup')
  })

  it('answers consent on its own page', async () => {
    assert.equal(await landed('/user/login/consent', () => api.answerConsent('allow')), '/prod/user/login/consent')
  })

  it('confirms sign-out from the sign-out page', async () => {
    Object.assign(globalThis, { window: { location: { search: '' } } })
    assert.equal(await landed('/user/logout', () => api.confirmLogout('csrf')), '/prod/user/logout/confirm')
  })
})

describe('what the page makes of the answer', () => {
  it("shows an account endpoint's own sentence", async () => {
    answer(400, '{"message":"Password does not meet the password requirements"}')
    const result = await account.confirmRecovery('someone@example.com', '123456', 'Password1')
    assert.equal(!result.ok && result.error.message, 'Password does not meet the password requirements')
  })

  it('shows the retryable generic error for a request that never arrived (negative)', async () => {
    globalThis.fetch = async () => { throw new TypeError('network down') }
    assert.deepEqual(await account.confirmRecovery('someone@example.com', '123456', 'Password1!'), { ok: false, error: describeError(null) })
  })

  it('reads a body that is not JSON as empty rather than failing (negative)', async () => {
    answer(200, 'OK')
    assert.deepEqual(await api.sendCode('someone@example.com', 'flow-1'), { ok: true, data: {} })
    answer(502, '<html>Bad Gateway</html>')
    assert.deepEqual(await api.sendCode('someone@example.com', 'flow-1'), { ok: false, error: describeError(null) })
  })

  it('shows the generic error for a finished step that names nowhere to go, never a trip to "undefined" (negative)', async () => {
    for (const body of ['{}', 'OK', 'null']) {
      answer(200, body)
      assert.deepEqual(await api.verifyCode('123456', 'flow-1'), { ok: false, error: describeError(null) }, body)
    }
  })

  it('passes on where a finished step goes', async () => {
    answer(200, '{"redirect_to":"https://app.example.com/cb?code=c"}')
    assert.deepEqual(await api.answerConsent('allow', 'flow-1'), { ok: true, data: { redirect_to: 'https://app.example.com/cb?code=c' } })
  })
})
