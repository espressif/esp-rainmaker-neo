/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { sigv4Request } from './sigv4-client'

vi.mock('@/stores/auth.store', () => ({
  useAuthStore: {
    getState: () => ({
      credentials: { accessKeyId: 'AKIDEXAMPLE', secretAccessKey: 'secret', sessionToken: 'token' },
      isCredentialsValid: () => true,
      clearCredentials: vi.fn(),
    }),
  },
}))
vi.mock('@/lib/config', () => ({ getAwsRegion: () => 'us-east-1' }))

const URL = 'https://api.example.com/v1/thing'

describe('sigv4Request', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('parses a JSON body', async () => {
    vi.mocked(fetch).mockResolvedValue(new Response('{"message":"ok"}', { status: 200 }))
    await expect(sigv4Request({ method: 'PUT', url: URL, body: {} })).resolves.toEqual({ message: 'ok' })
  })

  it('accepts a 204 with no body', async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }))
    await expect(sigv4Request({ method: 'DELETE', url: URL })).resolves.toBeUndefined()
  })
})
