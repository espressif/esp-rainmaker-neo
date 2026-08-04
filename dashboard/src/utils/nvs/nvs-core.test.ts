/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { toWireRows, type NvsRow } from './nvs-core'

describe('toWireRows', () => {
  it('maps each encoding to the right wire op', () => {
    const rows: NvsRow[] = [
      { key: 'ns', type: 'namespace', encoding: '', value: '' },
      { key: 'count', type: 'data', encoding: 'u32', value: 3840 },
      { key: 'name', type: 'data', encoding: 'string', value: 'RMNG' },
      { key: 'rand', type: 'data', encoding: 'hex2bin', value: 'deadbeef' },
      { key: 'pem', type: 'data', encoding: 'binary', value: 'cert-string' },
      { key: 'der', type: 'data', encoding: 'binary', value: new Uint8Array([0x30, 0x82, 0x01]) },
    ]
    expect(toWireRows(rows)).toEqual([
      { key: 'ns', op: 'ns', val: '' },
      { key: 'count', op: 'u32', val: '3840' },
      { key: 'name', op: 'str', val: 'RMNG' },
      { key: 'rand', op: 'hex2bin', val: 'deadbeef' },
      { key: 'pem', op: 'utf8bin', val: 'cert-string' },
      { key: 'der', op: 'rawbin', val: '308201' },
    ])
  })

  it('throws on an unsupported encoding', () => {
    const rows = [{ key: 'x', type: 'data', encoding: 'b64', value: 'y' }] as unknown as NvsRow[]
    expect(() => toWireRows(rows)).toThrow(/unsupported NVS encoding/)
  })
})
