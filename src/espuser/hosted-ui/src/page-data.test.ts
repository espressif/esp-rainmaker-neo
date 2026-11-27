/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { readPageData } from './page-data.ts'

const docWith = (text: string | null) =>
  ({ getElementById: () => (text === null ? null : { textContent: text }) }) as unknown as Document

describe('readPageData', () => {
  it('reads Go nil slices as empty lists, so a consent for no scopes still renders (negative)', () => {
    const page = readPageData(docWith(JSON.stringify({ providers: null, consent: { scopes: null } })))
    assert.deepEqual(page.providers, [])
    assert.deepEqual(page.consent?.scopes, [])
  })

  it('describes nothing rather than failing when the block is missing or broken (negative)', () => {
    for (const text of [null, '', '{not json']) {
      assert.deepEqual(readPageData(docWith(text)), { client: {}, providers: [], otp_enabled: false })
    }
  })
})
