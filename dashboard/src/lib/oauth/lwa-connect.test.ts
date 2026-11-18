/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { buildLwaAuthorizeUrl, evaluateLwaCallback, LwaConnectError, type LwaCallbackMessage } from './lwa-connect'

const response = (fields: Partial<LwaCallbackMessage>): LwaCallbackMessage => ({
  state: null,
  code: null,
  error: null,
  error_description: null,
  ...fields,
})

describe('buildLwaAuthorizeUrl', () => {
  it('asks Amazon for an authorization code with the skill-management scopes', () => {
    const url = new URL(buildLwaAuthorizeUrl('amzn1.application-oa2-client.x', 'https://dash.example.com/alexa-lwa-callback', 's1'))
    expect(url.origin + url.pathname).toBe('https://www.amazon.com/ap/oa')
    expect(Object.fromEntries(url.searchParams)).toEqual({
      client_id: 'amzn1.application-oa2-client.x',
      scope: 'alexa::ask:skills:readwrite alexa::ask:skills:test',
      response_type: 'code',
      redirect_uri: 'https://dash.example.com/alexa-lwa-callback',
      state: 's1',
    })
  })
})

describe('evaluateLwaCallback', () => {
  it('returns the code for this request', () => {
    expect(evaluateLwaCallback(response({ state: 's1', code: 'c' }), 's1')).toBe('c')
  })

  it('ignores a response to another request', () => {
    expect(evaluateLwaCallback(response({ state: 's2', code: 'c' }), 's1')).toBeNull()
  })

  it('reports a refused consent with Amazon\'s description', () => {
    const run = () => evaluateLwaCallback(response({ state: 's1', error: 'access_denied', error_description: 'User denied' }), 's1')
    expect(run).toThrow(LwaConnectError)
    expect(run).toThrow('User denied')
  })

  it('rejects a response with neither code nor error', () => {
    expect(() => evaluateLwaCallback(response({ state: 's1' }), 's1')).toThrow(expect.objectContaining({ reason: 'noCode' }) as Error)
  })
})
