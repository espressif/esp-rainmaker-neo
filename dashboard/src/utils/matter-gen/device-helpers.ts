/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { GenerateMatterDeviceParams } from './matter-device.types'
import { loadIssuer } from './matter-cert'
import { getChipTestCredentials } from './chip-test-credentials'
import type { NvsRow } from '../nvs/nvs-core'

export function resolveMatterIssuer(
  params: GenerateMatterDeviceParams,
  vendorId: number,
  productId: number,
): { issuer: ReturnType<typeof loadIssuer>; paiCertPem: string; cdDer?: Uint8Array } {
  const bundled = params.pai ? undefined : getChipTestCredentials(vendorId, productId)
  const paiCertPem = params.pai?.certPem ?? bundled?.paiCertPem
  const paiKeyPem = params.pai?.keyPem ?? bundled?.paiKeyPem
  if (!paiCertPem || !paiKeyPem) {
    throw new Error(
      `No PAI available for VID=0x${(vendorId & 0xffff).toString(16)} ` +
        `PID=0x${(productId & 0xffff).toString(16)}. Pass { pai: { certPem, keyPem } } ` +
        '(Matter does not allow self-signed DACs).',
    )
  }
  return {
    issuer: loadIssuer(paiCertPem, paiKeyPem),
    paiCertPem,
    cdDer: params.cdDer ?? bundled?.cdDer,
  }
}

function toHexLocal(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

function randomBytes(n: number): Uint8Array {
  const b = new Uint8Array(n)
  crypto.getRandomValues(b)
  return b
}

export function appendRainmakerRows(
  nvsRows: NvsRow[],
  params: GenerateMatterDeviceParams,
  clientId: string,
): void {
  if (!params.mqttHost) {
    return
  }
  nvsRows.push(
    { key: 'rmaker_creds', type: 'namespace', encoding: '', value: '' },
    { key: 'mqtt_host', type: 'data', encoding: 'binary', value: params.mqttHost },
    { key: 'mqtt_cred_host', type: 'data', encoding: 'binary', value: params.mqttCredHost ?? '' },
    { key: 'files_bucket', type: 'data', encoding: 'binary', value: params.filesBucket ?? '' },
    { key: 'client_id', type: 'data', encoding: 'binary', value: clientId },
    { key: 'random', type: 'data', encoding: 'hex2bin', value: toHexLocal(randomBytes(16)) },
  )
}
