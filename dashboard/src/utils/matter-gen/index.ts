/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Matter factory-data generator (standalone).
 *
 * Produces, per device, all the unique Matter data the firmware factory
 * partition needs — DAC/PAI, discriminator, passcode, SPAKE2+ verifier,
 * onboarding QR + manual pairing code — plus the `chip-factory` NVS rows
 * (optionally merged with a RainMaker `rmaker_creds` namespace).
 *
 * This mirrors what esp-matter-mfg-tool + factory_nvs_gen produce, but runs
 * fully in TypeScript (Node + browser). The actual NVS `.bin` packing is left
 * to the dashboard's existing Pyodide `nvs-partition-gen` step at integration.
 */

import {
  CommissioningFlow,
  generateManualPairingCode,
  generateQrPayload,
} from './setup-payload'
import { generateSpake2pVerifier } from './spake2p'
import { generateDac } from './matter-cert'
import { appendRainmakerRows, resolveMatterIssuer } from './device-helpers'
import { buildChipFactoryRows, type ChipFactoryParams } from './chip-factory-nvs'
import { pemToDer } from './asn1'
import {
  type GenerateMatterDeviceParams,
  type MatterDevice,
  MATTER_DEFAULT_VENDOR_ID,
  MATTER_DEFAULT_PRODUCT_ID,
  DEFAULT_DISCOVERY_CAPABILITIES,
} from './matter-device.types'

export type { NvsRow, NvsEncoding } from '../nvs/nvs-core'
export type { GenerateMatterDeviceParams, MatterDevice } from './matter-device.types'

const QR_BASE_URL = 'https://project-chip.github.io/connectedhomeip/qrcode.html?data='

// Passcodes disallowed by the Matter spec.
const INVALID_PASSCODES = new Set([
  0, 11111111, 22222222, 33333333, 44444444, 55555555, 66666666, 77777777, 88888888, 99999999,
  12345678, 87654321,
])

/** Uniform integer in [0, max) using rejection sampling on 32-bit randoms. */
function uniformBelow(max: number): number {
  const limit = Math.floor(0x100000000 / max) * max
  const buf = new Uint32Array(1)
  let x: number
  do {
    crypto.getRandomValues(buf)
    x = buf[0]
  } while (x >= limit)
  return x % max
}

/** Random valid passcode in [1, 99999998], avoiding the invalid set. */
export function randomPasscode(): number {
  let p = 1 + uniformBelow(99999998)
  if (INVALID_PASSCODES.has(p)) {p -= 1}
  return p
}

/** Random 12-bit discriminator. */
export function randomDiscriminator(): number {
  return uniformBelow(4096)
}

function randomBytes(n: number): Uint8Array {
  const b = new Uint8Array(n)
  crypto.getRandomValues(b)
  return b
}

function bytesToBase64(bytes: Uint8Array): string {
  let bin = ''
  for (const b of bytes) {bin += String.fromCharCode(b)}
  return btoa(bin)
}

export * from './setup-payload'
export * from './spake2p'
export * from './matter-cert'
export * from './chip-factory-nvs'

export { MATTER_DEFAULT_VENDOR_ID, MATTER_DEFAULT_PRODUCT_ID } from './matter-device.types'

export function generateMatterDevice(params: GenerateMatterDeviceParams = {}): MatterDevice {
  const vendorId = params.vendorId ?? MATTER_DEFAULT_VENDOR_ID
  const productId = params.productId ?? MATTER_DEFAULT_PRODUCT_ID
  const vendorName = params.vendorName ?? 'RMNG'
  const productName = params.productName ?? 'matter-sim'
  const hardwareVersion = params.hardwareVersion ?? 1
  const hardwareVersionString = params.hardwareVersionString ?? '1'
  const iterationCount = params.iterationCount ?? 10000
  const discoveryCapabilities = params.discoveryCapabilities ?? DEFAULT_DISCOVERY_CAPABILITIES
  const commissioningFlow = params.commissioningFlow ?? CommissioningFlow.Standard

  const passcode = params.passcode ?? randomPasscode()
  const discriminator = params.discriminator ?? randomDiscriminator()
  const salt = randomBytes(32)
  const saltB64 = bytesToBase64(salt)
  const rotatingIdUid = randomBytes(16)

  const { verifier, verifierB64 } = generateSpake2pVerifier(passcode, salt, iterationCount)

  const { issuer, paiCertPem, cdDer } = resolveMatterIssuer(params, vendorId, productId)

  const dac = generateDac(issuer, { vendorId, productId })

  const setup = {
    passcode,
    discriminator,
    vendorId,
    productId,
    commissioningFlow,
    discoveryCapabilities,
  }
  const qrPayload = generateQrPayload(setup)
  const manualPairingCode = generateManualPairingCode(setup)

  const chipParams: ChipFactoryParams = {
    discriminator,
    iterationCount,
    saltB64,
    verifierB64,
    dacCertDer: dac.certDer,
    dacKeyRaw: dac.privateScalar,
    dacPubRaw: dac.publicKeyRaw,
    paiCertDer: pemToDer(paiCertPem),
    cdDer,
    vendorId,
    productId,
    vendorName,
    productName,
    hardwareVersion,
    hardwareVersionString,
    rotatingIdUid,
  }
  const nvsRows = buildChipFactoryRows(chipParams)

  appendRainmakerRows(nvsRows, params, dac.commonName)

  return {
    thingName: dac.commonName,
    vendorId,
    productId,
    passcode,
    discriminator,
    salt,
    saltB64,
    iterationCount,
    verifier,
    verifierB64,
    qrPayload,
    qrUrl: QR_BASE_URL + qrPayload,
    manualPairingCode,
    rotatingIdUid,
    dac,
    pai: { certPem: paiCertPem },
    nvsRows,
  }
}
