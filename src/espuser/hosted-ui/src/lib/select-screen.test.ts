/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import type { PageData, Provider } from '../page-data.ts'
import { signInOptions, initialPool, screenFor, clientLabel, destinationName } from './select-screen.ts'

const pool = (over: Partial<Provider> = {}): Provider => ({ name: 'cognito', display_name: 'RainMaker', method: 'password', ...over })
const google = (over: Partial<Provider> = {}): Provider => ({ name: 'google', display_name: 'Google', brand: 'google', method: 'redirect', ...over })
const page = (over: Partial<PageData> = {}): PageData => ({ client: {}, providers: [], otp_enabled: false, ...over })

describe('signInOptions', () => {
  it('answers a password pool on this page and sends anything else away, in order, with its brand', () => {
    const corp = google({ name: 'corp', display_name: 'Corp SSO', brand: undefined })
    const options = signInOptions(page({ flow_id: 'fl_1', providers: [pool(), google(), pool({ name: 'insights', display_name: 'ESP Insights' }), corp] }))
    assert.deepEqual(options.pools.map(p => p.id), ['cognito', 'insights'])
    assert.deepEqual(options.redirects.map(r => [r.href, r.brand]), [
      ['login/federation/start?provider=google&flow_id=fl_1', 'google'],
      ['login/federation/start?provider=corp&flow_id=fl_1', undefined],
    ])
  })

  it('carries a provider logo to its button, for a redirect and a pool alike', () => {
    const logo = 'data:image/png;base64,iVBORw0KGgo='
    const options = signInOptions(page({ providers: [pool({ logo }), google({ name: 'microsoft', brand: undefined, logo })] }))
    assert.equal(options.pools[0]?.logo, logo)
    assert.equal(options.redirects[0]?.logo, logo)
  })

  it('offers the one-time code only when it is enabled', () => {
    assert.equal(signInOptions(page({ otp_enabled: true })).otp, true)
    assert.equal(signInOptions(page()).otp, false)
  })

  it('labels a provider by its display name, never by the registry key (negative)', () => {
    const cases: [Provider, string][] = [
      [pool({ display_name: 'Acme SSO' }), 'Acme SSO'],
      [pool({ display_name: 'Cognito' }), 'Email and password'],
      [pool({ display_name: undefined }), 'Email and password'],
      [google({ display_name: undefined }), 'Continue'],
    ]
    for (const [provider, label] of cases) {
      const { pools, redirects } = signInOptions(page({ providers: [provider] }))
      assert.equal([...pools, ...redirects][0]?.label, label, provider.display_name)
    }
  })
})

describe('initialPool', () => {
  it('starts on the pool the client hinted with idp, else the first, else none (negative)', () => {
    const pools = signInOptions(page({ providers: [pool(), pool({ name: 'insights' })] })).pools
    const cases: [string | undefined, string | null][] = [[undefined, 'cognito'], ['insights', 'insights'], ['google', 'cognito']]
    for (const [hint, chosen] of cases) assert.equal(initialPool(pools, hint), chosen, hint)
    assert.equal(initialPool([]), null)
  })
})

describe('screenFor', () => {
  it('shows an error first, then a question the server asks, then a finished sign-out, else signing in', () => {
    const error = { code: 'invalid_request', message: 'Bad request.' }
    const logout = { account: 'person@example.com', csrf_token: 't0k' }
    const consent = { scopes: ['openid'] }
    const cases: [Partial<PageData>, ReturnType<typeof screenFor>][] = [
      [{ error, logout, consent }, { kind: 'error', error }],
      [{ logout, providers: [pool()], otp_enabled: true }, { kind: 'logout', logout }],
      [{ logout, signed_out: true }, { kind: 'logout', logout }],
      [{ consent }, { kind: 'consent', consent }],
      [{ signed_out: true }, { kind: 'signed-out' }],
      [{ providers: [pool()] }, { kind: 'sign-in' }],
    ]
    for (const [over, screen] of cases) assert.deepEqual(screenFor(page(over)), screen)
  })
})

describe('clientLabel', () => {
  it('names the client the person is signing in to, and nothing for a blank name (negative)', () => {
    assert.equal(clientLabel(' ESP RainMaker '), 'ESP RainMaker')
    assert.equal(clientLabel(undefined), null)
    assert.equal(clientLabel('  '), null)
  })
})

describe('destinationName', () => {
  it('names a redirect by its brand, else by the label its button shows', () => {
    const { redirects } = signInOptions(page({ providers: [google({ display_name: 'Workspace' }), google({ name: 'staging', display_name: 'RainMaker Staging', brand: undefined })] }))
    assert.deepEqual(redirects.map(destinationName), ['Google', 'RainMaker Staging'])
  })

  it('never names a redirect by its registry key (negative)', () => {
    const { redirects } = signInOptions(page({ providers: [google({ name: 'corp', display_name: undefined, brand: undefined })] }))
    assert.equal(destinationName(redirects[0]!), 'your sign-in provider')
  })
})
