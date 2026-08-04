/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { NvsRow } from '../nvs/nvs-core'
import type { CommissioningFlow } from './setup-payload'
import { DISCOVERY_CAP_BLE } from './setup-payload'
import type { DacResult } from './matter-cert'

export interface GenerateMatterDeviceParams {
  vendorId?: number
  productId?: number
  vendorName?: string
  productName?: string
  hardwareVersion?: number
  hardwareVersionString?: string
  iterationCount?: number
  discoveryCapabilities?: number
  commissioningFlow?: CommissioningFlow
  passcode?: number
  discriminator?: number
  pai?: { certPem: string; keyPem: string }
  cdDer?: Uint8Array
  mqttHost?: string
  mqttCredHost?: string
  filesBucket?: string
}

export interface MatterDevice {
  thingName: string
  vendorId: number
  productId: number
  passcode: number
  discriminator: number
  salt: Uint8Array
  saltB64: string
  iterationCount: number
  verifier: Uint8Array
  verifierB64: string
  qrPayload: string
  qrUrl: string
  manualPairingCode: string
  rotatingIdUid: Uint8Array
  dac: DacResult
  pai: { certPem: string }
  nvsRows: NvsRow[]
}

export const MATTER_DEFAULT_VENDOR_ID = 0xfff2
export const MATTER_DEFAULT_PRODUCT_ID = 0x8001
export const DEFAULT_DISCOVERY_CAPABILITIES = DISCOVERY_CAP_BLE
