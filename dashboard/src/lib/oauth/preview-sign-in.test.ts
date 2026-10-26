/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { oauthClientsApi, type OAuthClient } from '@/api/oauth-clients'
import { openPreviewSignIn } from './preview-sign-in'

vi.mock('@/api/oauth-clients', () => ({
  oauthClientsApi: { list: vi.fn(), update: vi.fn() },
}))
vi.mock('@/lib/config', () => ({
  getAuthorizeUrl: () => 'https://auth.example.com/oauth2/authorize',
  getVaClientId: () => 'va-client',
}))
vi.mock('@/lib/auth', () => ({
  generatePkceParams: () => Promise.resolve({ verifier: 'verifier', challenge: 'challenge', state: 'state' }),
}))
vi.mock('@/lib/safe-storage', () => ({
  safeLocalStorage: { get: vi.fn(), set: vi.fn(), remove: vi.fn() },
}))

const ORIGIN = 'https://dashboard.example.com'

const registered: OAuthClient = {
  client_id: 'va-client',
  client_name: 'Voice assistant',
  client_type: 'confidential',
  redirect_uris: ['https://va.example.com/link'],
  post_logout_redirect_uris: ['https://va.example.com/signed-out'],
  grant_types: ['authorization_code', 'refresh_token'],
  scopes: ['openid', 'email', 'profile'],
  allowed_resources: ['https://api.example.com'],
  allowed_providers: ['cognito'],
  require_pkce: false,
  first_party: true,
  client_secret: 'secret',
  created_at: 1,
  updated_at: 2,
}

describe('openPreviewSignIn', () => {
  beforeEach(() => {
    vi.stubGlobal('window', { location: { origin: ORIGIN }, open: vi.fn() })
    vi.mocked(oauthClientsApi.list).mockResolvedValue({ clients: [registered] })
    vi.mocked(oauthClientsApi.update).mockImplementation((_id, body) =>
      Promise.resolve({ ...registered, redirect_uris: body.redirect_uris }),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('sends every mutable field back when it adds the preview callback', async () => {
    await openPreviewSignIn()

    expect(oauthClientsApi.update).toHaveBeenCalledTimes(1)
    const [clientId, body] = vi.mocked(oauthClientsApi.update).mock.calls[0]
    expect(clientId).toBe('va-client')
    expect(body).toEqual({
      client_name: 'Voice assistant',
      redirect_uris: ['https://va.example.com/link', `${ORIGIN}/oauth-preview`],
      post_logout_redirect_uris: ['https://va.example.com/signed-out'],
      grant_types: ['authorization_code', 'refresh_token'],
      scopes: ['openid', 'email', 'profile'],
      allowed_resources: ['https://api.example.com'],
      allowed_providers: ['cognito'],
      require_pkce: false,
      first_party: true,
    })
  })
})
