/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Matter bulk manufacturing zip builder.
 *
 * Mirrors `zip-builder.ts` (generateBulkMfgZip) as closely as possible but for
 * Matter-enabled devices: each node gets a DAC (signed by the bundled CHIP test
 * PAI), Matter commissioning data, and a merged Matter + RainMaker NVS factory
 * partition. The archive keeps the same top-level layout (common/, node_details/,
 * bin/, qrcode/) and the same `common/node_certs.csv` (node_id,certs,qrcode) so
 * the output drops straight into Bulk Node Registration.
 */

import JSZip from 'jszip'
import { requireFolder } from './jszip-helpers'
import { generateMatterDevice } from '@/utils/matter-gen'
import { generateNvsBin, preloadPyodide } from '@/utils/nvs/pyodide-browser'
import { MATTER_PARTITION_SIZE } from '@/utils/nvs/nvs-core'
import { generateQrPng } from './qr-gen'
import {
  generatePrefix,
  padIndex,
  csvEscape,
  type BulkMfgZipParams,
  type BulkMfgZipResult,
} from './zip-builder'

/**
 * Generate a complete bulk manufacturing zip for Matter-enabled test nodes.
 * Same params/result shape as generateBulkMfgZip so the panel can swap builders.
 */
export async function generateMatterBulkMfgZip({
  count,
  mqttHost,
  mqttCredHost = '',
  filesBucket = '',
  onProgress,
}: BulkMfgZipParams): Promise<BulkMfgZipResult> {
  const PREFIX = generatePrefix()
  const zip = new JSZip()
  const mfg = requireFolder(zip, PREFIX)

  // Warm up the Pyodide NVS packer while we begin generating.
  onProgress({ phase: 'init', message: 'Initializing...' })
  await preloadPyodide()

  interface MatterDeviceData {
    thingName: string
    dacCertPem: string
    qrPayload: string
    qrUrl: string
    manualPairingCode: string
    vendorId: number
    productId: number
    discriminator: number
    passcode: number
    dirName: string
  }

  const devices: MatterDeviceData[] = []
  let paiCertPem = ''

  for (let i = 1; i <= count; i++) {
    onProgress({
      phase: 'device',
      current: i,
      total: count,
      message: `Generating device ${i}/${count}...`,
    })

    const device = generateMatterDevice({ mqttHost, mqttCredHost, filesBucket })
    paiCertPem = device.pai.certPem
    const dirName = `node-${padIndex(i)}-${device.thingName}`

    // Merged Matter + RainMaker NVS factory partition.
    const nvsBin = await generateNvsBin(device.nvsRows, MATTER_PARTITION_SIZE)
    // Matter onboarding QR (the "MT:" payload).
    const qrPng = await generateQrPng(device.qrPayload)

    const nodeDir = requireFolder(requireFolder(mfg, 'node_details'), dirName)
    nodeDir.file('dac.crt', device.dac.certPem)
    nodeDir.file('dac.key', device.dac.keyPem)
    nodeDir.file('pai.crt', device.pai.certPem)
    nodeDir.file('qrcode.txt', device.qrPayload)
    nodeDir.file('qr_link.txt', device.qrUrl + '\n')
    nodeDir.file('manual_code.txt', device.manualPairingCode + '\n')

    requireFolder(mfg, 'bin').file(`${dirName}.bin`, nvsBin)
    requireFolder(mfg, 'qrcode').file(`${dirName}.png`, qrPng)

    devices.push({
      thingName: device.thingName,
      dacCertPem: device.dac.certPem,
      qrPayload: device.qrPayload,
      qrUrl: device.qrUrl,
      manualPairingCode: device.manualPairingCode,
      vendorId: device.vendorId,
      productId: device.productId,
      discriminator: device.discriminator,
      passcode: device.passcode,
      dirName,
    })
  }

  // Build common files (mirrors the non-Matter archive).
  onProgress({ phase: 'zip', message: 'Building archive...' })

  const common = requireFolder(mfg, 'common')
  common.file('endpoint.txt', mqttHost)
  common.file('mqtt_cred_host.txt', mqttCredHost)
  common.file('files_bucket.txt', filesBucket)
  // PAI is the CA-equivalent for Matter (shared across the batch).
  common.file('pai.crt', paiCertPem)

  // node_ids.csv
  common.file('node_ids.csv', 'node_id\n' + devices.map((d) => d.thingName).join('\n') + '\n')

  // node_certs.csv — primary output used for registration (DAC cert per node).
  const nodeCertsRows = devices.map(
    (d) => `${d.thingName},${csvEscape(d.dacCertPem)},${csvEscape(d.qrPayload)}`,
  )
  const nodeCertsCsv = 'node_id,certs,qrcode\n' + nodeCertsRows.join('\n') + '\n'
  common.file('node_certs.csv', nodeCertsCsv)

  // values.csv — Matter commissioning values per node.
  const valuesHeader =
    'id,node_id,vendor_id,product_id,discriminator,passcode,manual_code,qrcode'
  const valuesRows = devices.map((d, idx) =>
    [
      idx + 1,
      d.thingName,
      `0x${(d.vendorId & 0xffff).toString(16)}`,
      `0x${(d.productId & 0xffff).toString(16)}`,
      d.discriminator,
      d.passcode,
      d.manualPairingCode,
      csvEscape(d.qrPayload),
    ].join(','),
  )
  common.file('values.csv', valuesHeader + '\n' + valuesRows.join('\n') + '\n')

  const blob = await zip.generateAsync({ type: 'blob' })
  return { blob, prefix: PREFIX, nodeCertsCsv }
}
