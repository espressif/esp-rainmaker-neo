/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Matter onboarding setup payload — QR code string and manual pairing code.
 *
 * Mirrors connectedhomeip / esp-matter-mfg-tool `SetupPayload`
 * (`generate_qrcode` / `generate_manualcode`).
 */

import { base38Encode } from './base38'
import { verhoeffCheckDigit } from './verhoeff'

export enum CommissioningFlow {
  Standard = 0,
  UserActionRequired = 1,
  Custom = 2,
}

// Discovery-capability bitmask (a.k.a. rendezvous information).
export const DISCOVERY_CAP_SOFT_AP = 1 << 0
export const DISCOVERY_CAP_BLE = 1 << 1
export const DISCOVERY_CAP_ON_NETWORK = 1 << 2

export interface SetupPayloadParams {
  /** 27-bit setup passcode / PIN. */
  passcode: number
  /** 12-bit discriminator. */
  discriminator: number
  vendorId: number
  productId: number
  commissioningFlow?: CommissioningFlow
  /** Discovery-capability bitmask; defaults to BLE (matches esp-matter-mfg-tool). */
  discoveryCapabilities?: number
}

const QR_VERSION = 0

/**
 * Pack the payload into the 88-bit / 11-byte little-endian field layout and
 * Base38-encode it with the "MT:" prefix.
 *
 * Field order (LSB → MSB): version(3) | vendorId(16) | productId(16) |
 * commissioningFlow(2) | discoveryCapabilities(8) | discriminator(12) |
 * passcode(27) | padding(4).
 */
export function generateQrPayload(p: SetupPayloadParams): string {
  const flow = p.commissioningFlow ?? CommissioningFlow.Standard
  const discovery = p.discoveryCapabilities ?? DISCOVERY_CAP_BLE

  let value = 0n
  let shift = 0n
  const put = (v: number, bits: number) => {
    const mask = (1n << BigInt(bits)) - 1n
    value |= (BigInt(v) & mask) << shift
    shift += BigInt(bits)
  }

  put(QR_VERSION, 3)
  put(p.vendorId, 16)
  put(p.productId, 16)
  put(flow, 2)
  put(discovery, 8)
  put(p.discriminator, 12)
  put(p.passcode, 27)
  put(0, 4) // padding → 88 bits total

  const bytes = new Uint8Array(11)
  for (let i = 0; i < 11; i++) {bytes[i] = Number((value >> BigInt(8 * i)) & 0xffn)}

  return 'MT:' + base38Encode(bytes)
}

/**
 * Build the decimal manual pairing code. Standard flow → 11 digits
 * (10 + Verhoeff check digit); other flows additionally encode VID/PID
 * → 21 digits.
 */
export function generateManualPairingCode(p: SetupPayloadParams): string {
  const flow = p.commissioningFlow ?? CommissioningFlow.Standard
  const shortDiscriminator = (p.discriminator >> 8) & 0x0f
  const vidPidPresent = flow === CommissioningFlow.Standard ? 0 : 1

  const chunk1 = ((shortDiscriminator >> 2) & 0x03) | (vidPidPresent << 2)
  const chunk2 = (p.passcode & 0x3fff) | ((shortDiscriminator & 0x03) << 14)
  const chunk3 = (p.passcode >> 14) & 0x1fff

  let digits =
    String(chunk1) +
    String(chunk2).padStart(5, '0') +
    String(chunk3).padStart(4, '0')

  if (flow !== CommissioningFlow.Standard) {
    digits += String(p.vendorId).padStart(5, '0') + String(p.productId).padStart(5, '0')
  }

  return digits + verhoeffCheckDigit(digits)
}
