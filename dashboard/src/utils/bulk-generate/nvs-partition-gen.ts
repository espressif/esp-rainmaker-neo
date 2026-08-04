/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * RainMaker manufacturing NVS rows.
 *
 * The NVS packing engine (Pyodide + esp-idf-nvs-partition-gen) lives in
 * `utils/nvs`. This module only builds the RainMaker `rmaker_creds` rows and
 * delegates packing to the browser adapter, so the binary stays byte-identical
 * to the previous hardcoded implementation.
 */

import { generateNvsBin } from '../nvs/pyodide-browser'
import { RMAKER_PARTITION_SIZE, type NvsRow } from '../nvs/nvs-core'

export interface MfgNvsParams {
  nodeId: string
  privateKey: string
  certificate: string
  mqttHost: string
  mqttCredHost?: string
  filesBucket?: string
  random: string
}

/**
 * Build the RainMaker `rmaker_creds` rows. Order and encodings are preserved
 * exactly so the generated binary is byte-identical to the previous hardcoded
 * implementation.
 */
export function buildRmakerNvsRows(params: MfgNvsParams): NvsRow[] {
  return [
    { key: 'rmaker_creds', type: 'namespace', encoding: '', value: '' },
    { key: 'node_id', type: 'data', encoding: 'binary', value: params.nodeId },
    { key: 'mqtt_host', type: 'data', encoding: 'binary', value: params.mqttHost },
    { key: 'mqtt_cred_host', type: 'data', encoding: 'binary', value: params.mqttCredHost || '' },
    { key: 'files_bucket', type: 'data', encoding: 'binary', value: params.filesBucket || '' },
    { key: 'client_cert', type: 'data', encoding: 'binary', value: params.certificate },
    { key: 'client_key', type: 'data', encoding: 'binary', value: params.privateKey },
    { key: 'random', type: 'data', encoding: 'hex2bin', value: params.random },
  ]
}

/**
 * Generate an NVS partition binary for manufacturing (0x3000 / 12KB partition).
 */
export async function generateMfgNvsPartitionBin(params: MfgNvsParams): Promise<Uint8Array> {
  return generateNvsBin(buildRmakerNvsRows(params), RMAKER_PARTITION_SIZE)
}
