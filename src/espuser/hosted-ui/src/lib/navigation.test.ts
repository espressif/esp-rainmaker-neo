/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { opensHere, type ClickKeys } from './navigation.ts'

const click = (over: Partial<ClickKeys> = {}): ClickKeys => ({ button: 0, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...over })

describe('opensHere', () => {
  it('reads a plain primary click, including Enter on a focused link, as leaving this page', () => {
    assert.equal(opensHere(click()), true)
  })

  it('reads a modified or middle click as opening elsewhere, so the page stays usable (negative)', () => {
    const elsewhere: Partial<ClickKeys>[] = [{ metaKey: true }, { ctrlKey: true }, { shiftKey: true }, { altKey: true }, { button: 1 }]
    for (const over of elsewhere) assert.equal(opensHere(click(over)), false, JSON.stringify(over))
  })
})
