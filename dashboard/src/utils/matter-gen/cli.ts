#!/usr/bin/env node
/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * matter-gen CLI — generate the same per-device factory artifacts as
 * rmng-sdk `extras/tools/factory_autoreg/factory_autoreg.py --matter`, but
 * driven entirely by this TypeScript library.
 *
 * Run with tsx:
 *   npx tsx src/utils/matter-gen/cli.ts --matter -n 3 --mqtt-host <iot-endpoint>
 *
 * The NVS `.bin` is packed with Pyodide-in-Node — the identical WASM runtime
 * and `esp-idf-nvs-partition-gen` module the dashboard UI runs in the browser —
 * so the binaries are byte-equivalent and no system Python is required. The
 * first run micropip-installs the package from PyPI (needs network), then it's
 * cached for the session.
 *
 * Registration with the admin API is intentionally NOT performed (no admin
 * credentials here); artifacts are emitted ready to register (DAC as `cert`,
 * PAI as `ca_cert`).
 */

import { parseArgs } from 'node:util'
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'

import { generateMatterDevice, type MatterDevice } from './index'
import {
  DISCOVERY_CAP_BLE,
  DISCOVERY_CAP_ON_NETWORK,
  DISCOVERY_CAP_SOFT_AP,
} from './setup-payload'
import { generateNvsBinNode } from '../nvs/pyodide-node'

const USAGE = `matter-gen — Matter factory data generator (factory_autoreg parity)

Usage:
  npx tsx src/utils/matter-gen/cli.ts [options]

Options:
  --matter                 Matter mode (default/only mode of this CLI)
  -n, --count <N>          Number of devices to generate (default: 1)
  --output-dir <dir>       Output root (default: ./outputs)
  --account-id <id>        Account segment in the output path (default: local)
  --vendor-id <hex|dec>    Matter vendor id (default: 0xFFF2)
  --product-id <hex|dec>   Matter product id (default: 0x8001)
  --part-label <label>     Factory partition label / bin name (default: fctry)
  --partition-size <hex>   Partition size (default: 0x6000)
  --mqtt-host <host>       RainMaker IoT endpoint (merges rmaker_creds rows)
  --iteration-count <N>    SPAKE2+ iteration count (default: 10000)
  --discovery <mode>       ble | onnetwork | softap (default: ble)
  --pai-cert <path>        PAI cert PEM (override; default = bundled CHIP test
                           PAI for the VID/PID, e.g. FFF2/8001)
  --pai-key <path>         PAI key PEM (required with --pai-cert)
  --cd <path>              Certification Declaration (.der) for cert-dclrn
  --no-bin                 Skip NVS .bin packing
  -h, --help               Show this help

Output layout (per device):
  <output-dir>/<account-id>/matter/<thing-name>/
    dac_key.pem  dac_cert.pem  pai_cert.pem  qr_link.txt
    factory_nvs_input.json  registration.json  esp-idf/<part-label>.bin
`

type CliArgValues = {
  matter?: boolean
  count?: string
  'output-dir'?: string
  'account-id'?: string
  'vendor-id'?: string
  'product-id'?: string
  'part-label'?: string
  'partition-size'?: string
  'mqtt-host'?: string
  'iteration-count'?: string
  discovery?: string
  'pai-cert'?: string
  'pai-key'?: string
  cd?: string
  'no-bin'?: boolean
  help?: boolean
}

interface CliConfig {
  count: number
  vendorId: number
  productId: number
  iterationCount: number
  partitionSize: number
  discoveryCapabilities: number
  partLabel: string
  outputRoot: string
  mqttHost?: string
  skipBin: boolean
  pai?: { certPem: string; keyPem: string }
  cdDer?: Uint8Array
}

function parseIntFlexible(value: string, name: string): number {
  const n = value.startsWith('0x') || value.startsWith('0X') ? parseInt(value, 16) : Number(value)
  if (!Number.isFinite(n)) {throw new Error(`invalid number for ${name}: ${value}`)}
  return n
}

function discoveryMask(mode: string): number {
  switch (mode) {
    case 'ble':
      return DISCOVERY_CAP_BLE
    case 'onnetwork':
      return DISCOVERY_CAP_ON_NETWORK
    case 'softap':
      return DISCOVERY_CAP_SOFT_AP
    default:
      throw new Error(`invalid --discovery '${mode}' (ble|onnetwork|softap)`)
  }
}

function parsePaiFromCliArgs(values: CliArgValues): { certPem: string; keyPem: string } | undefined {
  if (values['pai-cert'] && values['pai-key']) {
    return {
      certPem: readFileSync(resolve(values['pai-cert']), 'utf-8'),
      keyPem: readFileSync(resolve(values['pai-key']), 'utf-8'),
    }
  }
  if (values['pai-cert'] || values['pai-key']) {
    throw new Error('provide both --pai-cert and --pai-key, or neither')
  }
  return undefined
}

function parseCliConfig(values: CliArgValues): CliConfig {
  const count = parseIntFlexible(values.count ?? '1', '--count')
  if (count < 1) {throw new Error('--count must be >= 1')}

  const pai = parsePaiFromCliArgs(values)

  return {
    count,
    vendorId: parseIntFlexible(values['vendor-id'] ?? '0xFFF2', '--vendor-id'),
    productId: parseIntFlexible(values['product-id'] ?? '0x8001', '--product-id'),
    iterationCount: parseIntFlexible(values['iteration-count'] ?? '10000', '--iteration-count'),
    partitionSize: parseIntFlexible(values['partition-size'] ?? '0x6000', '--partition-size'),
    discoveryCapabilities: discoveryMask(values.discovery ?? 'ble'),
    partLabel: values['part-label'] ?? 'fctry',
    outputRoot: resolve(values['output-dir'] ?? './outputs', values['account-id'] ?? 'local', 'matter'),
    mqttHost: values['mqtt-host'],
    skipBin: values['no-bin'] ?? false,
    pai,
    cdDer: values.cd ? new Uint8Array(readFileSync(resolve(values.cd))) : undefined,
  }
}

async function writeDeviceArtifacts(
  config: CliConfig,
  device: MatterDevice,
): Promise<Record<string, unknown>> {
  const outDir = join(config.outputRoot, device.thingName)
  const espIdfDir = join(outDir, 'esp-idf')
  mkdirSync(espIdfDir, { recursive: true })

  process.stdout.write(`Thing name (DAC CN): ${device.thingName}\n`)
  process.stdout.write(`Output directory: ${outDir}\n`)

  writeFileSync(join(outDir, 'dac_key.pem'), device.dac.keyPem)
  writeFileSync(join(outDir, 'dac_cert.pem'), device.dac.certPem)
  writeFileSync(join(outDir, 'pai_cert.pem'), device.pai.certPem)
  writeFileSync(join(outDir, 'qr_link.txt'), device.qrUrl + '\n')

  const binPath = join(espIdfDir, `${config.partLabel}.bin`)
  let binWritten = false
  if (!config.skipBin) {
    try {
      const bin = await generateNvsBinNode(device.nvsRows, config.partitionSize)
      writeFileSync(binPath, bin)
      binWritten = true
      process.stdout.write(`  Factory NVS: ${binPath} (${bin.length} bytes)\n`)
    } catch (err) {
      console.warn(
        `  Warning: NVS .bin not generated (${err instanceof Error ? err.message : String(err)}).`,
      )
    }
  }

  const factoryInput = {
    mqtt_host: config.mqttHost ?? '',
    node_id: device.thingName,
    dac_key: join(outDir, 'dac_key.pem'),
    dac_cert: join(outDir, 'dac_cert.pem'),
    matter_factory_bin: binWritten ? binPath : '',
    qr_payload: device.qrPayload,
    manual_pairing_code: device.manualPairingCode,
    discriminator: device.discriminator,
    passcode: device.passcode,
    vendor_id: device.vendorId,
    product_id: device.productId,
  }
  writeFileSync(
    join(outDir, 'factory_nvs_input.json'),
    JSON.stringify(factoryInput, null, 4) + '\n',
  )

  const registration = {
    thing_name: device.thingName,
    node_id: device.thingName,
    output_dir: outDir,
    matter: true,
    qr_link: device.qrUrl,
    registered: false,
  }
  writeFileSync(join(outDir, 'registration.json'), JSON.stringify(registration, null, 2) + '\n')

  return {
    thing_name: device.thingName,
    node_id: device.thingName,
    output_dir: outDir,
    qr_link: device.qrUrl,
    manual_pairing_code: device.manualPairingCode,
  }
}

async function main(): Promise<number> {
  const { values } = parseArgs({
    options: {
      matter: { type: 'boolean', default: false },
      count: { type: 'string', short: 'n', default: '1' },
      'output-dir': { type: 'string', default: './outputs' },
      'account-id': { type: 'string', default: 'local' },
      'vendor-id': { type: 'string', default: '0xFFF2' },
      'product-id': { type: 'string', default: '0x8001' },
      'part-label': { type: 'string', default: 'fctry' },
      'partition-size': { type: 'string', default: '0x6000' },
      'mqtt-host': { type: 'string' },
      'iteration-count': { type: 'string', default: '10000' },
      discovery: { type: 'string', default: 'ble' },
      'pai-cert': { type: 'string' },
      'pai-key': { type: 'string' },
      cd: { type: 'string' },
      'no-bin': { type: 'boolean', default: false },
      help: { type: 'boolean', short: 'h', default: false },
    },
    allowPositionals: false,
  })

  if (values.help) {
    process.stdout.write(USAGE)
    return 0
  }

  const config = parseCliConfig(values)
  const batch: Array<Record<string, unknown>> = []

  for (let i = 0; i < config.count; i++) {
    if (config.count > 1) {
      process.stdout.write(`\n--- [${i + 1}/${config.count}] ---\n`)
    }

    const device = generateMatterDevice({
      vendorId: config.vendorId,
      productId: config.productId,
      iterationCount: config.iterationCount,
      discoveryCapabilities: config.discoveryCapabilities,
      pai: config.pai,
      cdDer: config.cdDer,
      mqttHost: config.mqttHost,
    })

    batch.push(await writeDeviceArtifacts(config, device))
  }

  if (config.count > 1) {
    mkdirSync(config.outputRoot, { recursive: true })
    const batchPath = join(config.outputRoot, 'batch_summary.json')
    writeFileSync(batchPath, JSON.stringify(batch, null, 2) + '\n')
    process.stdout.write(`\nWrote batch summary: ${batchPath}\n`)
  }

  return 0
}

main()
  .then((code) => process.exit(code))
  .catch((err) => {
    console.error(`ERROR: ${err instanceof Error ? err.message : String(err)}`)
    process.exit(1)
  })
