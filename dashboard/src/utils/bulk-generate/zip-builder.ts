/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Bulk manufacturing zip builder.
 * Orchestrates CA/device cert generation, QR codes, NVS binaries,
 * and packages everything into a zip matching the admin CLI output structure.
 * Ported from esp_rainmaker_dashboard/src/utils/bulkGenerate/zipBuilder.js
 */

import JSZip from 'jszip'
import { requireFolder } from './jszip-helpers'
import { generateCA, generateDeviceCert } from './cert-gen'
import { buildQrPayload, generateQrPng } from './qr-gen'
import { generateMfgNvsPartitionBin } from './nvs-partition-gen'
import { preloadPyodide } from '../nvs/pyodide-browser'

export function generatePrefix(): string {
  const now = new Date()
  const ts =
    now.getFullYear().toString() +
    String(now.getMonth() + 1).padStart(2, '0') +
    String(now.getDate()).padStart(2, '0') +
    '-' +
    String(now.getHours()).padStart(2, '0') +
    String(now.getMinutes()).padStart(2, '0') +
    String(now.getSeconds()).padStart(2, '0')
  return `Mfg-${ts}`
}

export function padIndex(i: number): string {
  return String(i).padStart(6, '0')
}

function toHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

function buildConfigCsv(): string {
  return [
    'rmaker_creds,namespace,',
    'node_id,data,binary',
    'mqtt_host,data,binary',
    'mqtt_cred_host,data,binary',
    'files_bucket,data,binary',
    'client_cert,file,binary',
    'client_key,file,binary',
    'random,data,hex2bin',
    '',
  ].join('\n')
}

export function csvEscape(val: string): string {
  if (val.includes('"') || val.includes(',') || val.includes('\n')) {
    return '"' + val.replace(/"/g, '""') + '"'
  }
  return val
}

export interface ProgressInfo {
  phase: 'init' | 'ca' | 'device' | 'zip' | 'done'
  message: string
  current?: number
  total?: number
}

export interface BulkMfgZipParams {
  count: number
  mqttHost: string
  mqttCredHost?: string
  filesBucket?: string
  onProgress: (info: ProgressInfo) => void
}

export interface BulkMfgZipResult {
  blob: Blob
  prefix: string
  nodeCertsCsv: string
}

/**
 * Generate a complete bulk manufacturing zip.
 */
export async function generateBulkMfgZip({
  count,
  mqttHost,
  mqttCredHost = '',
  filesBucket = '',
  onProgress,
}: BulkMfgZipParams): Promise<BulkMfgZipResult> {
  const PREFIX = generatePrefix()
  const zip = new JSZip()
  const mfg = requireFolder(zip, PREFIX)

  // Start Pyodide loading in parallel with CA generation
  onProgress({ phase: 'init', message: 'Initializing...' })
  const pyodidePromise = preloadPyodide()

  // Generate CA
  onProgress({ phase: 'ca', message: 'Generating CA certificate...' })
  const { caCertPem, caKeyPem, caKey, issuerDer } = await generateCA()

  interface DeviceData {
    nodeId: string
    certPem: string
    keyPem: string
    randomHex: string
    dirName: string
    payloadString: string
  }

  const devices: DeviceData[] = []

  for (let i = 1; i <= count; i++) {
    onProgress({
      phase: 'device',
      current: i,
      total: count,
      message: `Generating device ${i}/${count}...`,
    })

    const nodeId = crypto.randomUUID()
    const dirName = `node-${padIndex(i)}-${nodeId}`

    // Generate random (64 bytes -> 128 hex chars)
    const randomBytes = new Uint8Array(64)
    crypto.getRandomValues(randomBytes)
    const randomHex = toHex(randomBytes)

    // Generate device cert + key
    const { certPem, keyPem } = await generateDeviceCert(caKey, issuerDer, nodeId)

    // Build QR payload
    const payloadString = buildQrPayload(randomHex)

    // Generate QR code PNG
    const qrPng = await generateQrPng(payloadString)

    // Generate NVS binary (wait for Pyodide on first device)
    await pyodidePromise
    const nvsBin = await generateMfgNvsPartitionBin({
      nodeId,
      privateKey: keyPem,
      certificate: certPem,
      mqttHost,
      mqttCredHost,
      filesBucket,
      random: randomHex,
    })

    // Build per-device CSV
    const deviceCsv = [
      'key,type,encoding,value',
      'rmaker_creds,namespace,,',
      `node_id,data,binary,${nodeId}`,
      `mqtt_host,data,binary,${mqttHost}`,
      `mqtt_cred_host,data,binary,${mqttCredHost}`,
      `files_bucket,data,binary,${filesBucket}`,
      `client_cert,file,binary,${PREFIX}/node_details/${dirName}/node.crt`,
      `client_key,file,binary,${PREFIX}/node_details/${dirName}/node.key`,
      `random,data,hex2bin,${randomHex}`,
      '',
    ].join('\n')

    // Add files to zip
    const nodeDir = requireFolder(requireFolder(mfg, 'node_details'), dirName)
    nodeDir.file('node.crt', certPem)
    nodeDir.file('node.key', keyPem)
    nodeDir.file('random.txt', randomHex)
    nodeDir.file('qrcode.txt', payloadString)

    requireFolder(mfg, 'bin').file(`${dirName}.bin`, nvsBin)
    requireFolder(mfg, 'csv').file(`${dirName}.csv`, deviceCsv)
    requireFolder(mfg, 'qrcode').file(`${dirName}.png`, qrPng)

    devices.push({ nodeId, certPem, keyPem, randomHex, dirName, payloadString })
  }

  // Build common files
  onProgress({ phase: 'zip', message: 'Building archive...' })

  const common = requireFolder(mfg, 'common')
  common.file('config.csv', buildConfigCsv())
  common.file('endpoint.txt', mqttHost)
  common.file('mqtt_cred_host.txt', mqttCredHost)
  common.file('files_bucket.txt', filesBucket)
  common.file('ca.crt', caCertPem)
  common.file('ca.key', caKeyPem)

  // node_ids.csv
  const nodeIdsCsv = 'node_id\n' + devices.map((d) => d.nodeId).join('\n') + '\n'
  common.file('node_ids.csv', nodeIdsCsv)

  // node_certs.csv — primary output used for registration
  const nodeCertsRows = devices.map(
    (d) => `${d.nodeId},${csvEscape(d.certPem)},${csvEscape(d.payloadString)}`,
  )
  const nodeCertsCsv = 'node_id,certs,qrcode\n' + nodeCertsRows.join('\n') + '\n'
  common.file('node_certs.csv', nodeCertsCsv)

  // values.csv
  const valuesHeader = 'id,node_id,mqtt_host,mqtt_cred_host,files_bucket,client_cert,client_key,random,qrcode'
  const valuesRows = devices.map(
    (d, idx) =>
      [
        idx + 1,
        d.nodeId,
        mqttHost,
        mqttCredHost,
        filesBucket,
        `${PREFIX}/node_details/${d.dirName}/node.crt`,
        `${PREFIX}/node_details/${d.dirName}/node.key`,
        d.randomHex,
        csvEscape(d.payloadString),
      ].join(','),
  )
  const valuesCsv = valuesHeader + '\n' + valuesRows.join('\n') + '\n'
  common.file('values.csv', valuesCsv)

  const blob = await zip.generateAsync({ type: 'blob' })
  return { blob, prefix: PREFIX, nodeCertsCsv }
}
