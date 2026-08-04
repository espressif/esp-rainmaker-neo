/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import { buildRmakerNvsRows } from './nvs-partition-gen'
import { generateMatterDevice } from '../matter-gen'
import { toWireRows } from '../nvs/nvs-core'

describe('RainMaker rows (legacy parity)', () => {
  it('preserves the exact key order and wire ops of the old hardcoded packer', () => {
    const rows = buildRmakerNvsRows({
      nodeId: 'node123',
      privateKey: 'KEY',
      certificate: 'CERT',
      mqttHost: 'host',
      mqttCredHost: 'credhost',
      filesBucket: 'bucket',
      random: 'abcd1234',
    })
    expect(toWireRows(rows)).toEqual([
      { key: 'rmaker_creds', op: 'ns', val: '' },
      { key: 'node_id', op: 'utf8bin', val: 'node123' },
      { key: 'mqtt_host', op: 'utf8bin', val: 'host' },
      { key: 'mqtt_cred_host', op: 'utf8bin', val: 'credhost' },
      { key: 'files_bucket', op: 'utf8bin', val: 'bucket' },
      { key: 'client_cert', op: 'utf8bin', val: 'CERT' },
      { key: 'client_key', op: 'utf8bin', val: 'KEY' },
      { key: 'random', op: 'hex2bin', val: 'abcd1234' },
    ])
  })
})

describe('Matter rows through the shared packer', () => {
  it('normalizes chip-factory + rmaker_creds rows correctly', () => {
    const device = generateMatterDevice({ mqttHost: 'host-ats.iot.us-east-1.amazonaws.com' })
    const wire = toWireRows(device.nvsRows)
    const byKey = new Map(wire.map((w) => [w.key, w]))

    expect(byKey.get('chip-factory')?.op).toBe('ns')
    expect(byKey.get('discriminator')).toEqual({
      key: 'discriminator',
      op: 'u32',
      val: String(device.discriminator),
    })
    expect(byKey.get('salt')).toEqual({ key: 'salt', op: 'str', val: device.saltB64 })
    expect(byKey.get('verifier')).toEqual({ key: 'verifier', op: 'str', val: device.verifierB64 })
    expect(byKey.get('rd-id-uid')?.op).toBe('hex2bin')

    // DER blobs become rawbin and the hex round-trips back to the cert bytes.
    const dacWire = byKey.get('dac-cert')
    expect(dacWire?.op).toBe('rawbin')
    const hexPairs = dacWire?.val.match(/../g)
    expect(hexPairs).toBeDefined()
    if (!hexPairs) {
      return
    }
    const decoded = Uint8Array.from(hexPairs.map((h) => parseInt(h, 16)))
    expect(Array.from(decoded)).toEqual(Array.from(device.dac.certDer))

    // merged RainMaker namespace present after chip-factory
    expect(byKey.get('rmaker_creds')?.op).toBe('ns')
    expect(byKey.get('client_id')).toEqual({ key: 'client_id', op: 'utf8bin', val: device.thingName })
  })
})
