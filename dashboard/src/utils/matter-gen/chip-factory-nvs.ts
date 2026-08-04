/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Assemble the `chip-factory` NVS namespace entries for a Matter device.
 *
 * Mirrors the rows esp-matter-mfg-tool writes (`chip_nvs.py` +
 * `mfg_tool.py::chip_factory_update`). Entries are returned as a structured
 * list so the dashboard's existing Pyodide `nvs-partition-gen` step can write
 * them via `write_entry`, and so the merged binary can include the RainMaker
 * `rmaker_creds` namespace alongside.
 *
 * Key names / types / encodings match the tool exactly (e.g. `hardware-ver`
 * not `hw-ver`; `salt`/`verifier` stored as their base64 *strings*;
 * `discriminator`/`vendor-id`/`product-id`/`hardware-ver` as u32;
 * `rd-id-uid` as hex2bin).
 */

import type { NvsEncoding, NvsRow } from '../nvs/nvs-core'

export const CHIP_FACTORY_NAMESPACE = 'chip-factory'

export interface ChipFactoryParams {
  discriminator: number
  iterationCount: number
  saltB64: string
  verifierB64: string
  dacCertDer: Uint8Array
  /** Raw 32-byte DAC private scalar (big-endian). */
  dacKeyRaw: Uint8Array
  /** Uncompressed DAC public point (0x04 || X || Y, 65 bytes). */
  dacPubRaw: Uint8Array
  paiCertDer: Uint8Array
  /** Certification Declaration, DER. Optional (omitted if not provisioned). */
  cdDer?: Uint8Array
  vendorId: number
  productId: number
  vendorName: string
  productName: string
  hardwareVersion: number
  hardwareVersionString: string
  /** 16-byte rotating-device-id unique id. */
  rotatingIdUid: Uint8Array
  serialNumber?: string
}

const toHex = (bytes: Uint8Array): string =>
  Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')

export function buildChipFactoryRows(p: ChipFactoryParams): NvsRow[] {
  const rows: NvsRow[] = [{ key: CHIP_FACTORY_NAMESPACE, type: 'namespace', encoding: '', value: '' }]

  const data = (key: string, encoding: NvsEncoding, value: string | number | Uint8Array) =>
    rows.push({ key, type: 'data', encoding, value })

  // Commissioning data.
  data('discriminator', 'u32', p.discriminator)
  data('iteration-count', 'u32', p.iterationCount)
  data('salt', 'string', p.saltB64)
  data('verifier', 'string', p.verifierB64)

  // Device attestation (DER blobs + raw key material).
  data('dac-cert', 'binary', p.dacCertDer)
  data('dac-key', 'binary', p.dacKeyRaw)
  data('dac-pub-key', 'binary', p.dacPubRaw)
  data('pai-cert', 'binary', p.paiCertDer)
  if (p.cdDer) {data('cert-dclrn', 'binary', p.cdDer)}

  // Device instance information.
  data('vendor-id', 'u32', p.vendorId)
  data('vendor-name', 'string', p.vendorName)
  data('product-id', 'u32', p.productId)
  data('product-name', 'string', p.productName)
  data('hardware-ver', 'u32', p.hardwareVersion)
  data('hw-ver-str', 'string', p.hardwareVersionString)
  data('rd-id-uid', 'hex2bin', toHex(p.rotatingIdUid))
  if (p.serialNumber) {data('serial-num', 'string', p.serialNumber)}

  return rows
}
