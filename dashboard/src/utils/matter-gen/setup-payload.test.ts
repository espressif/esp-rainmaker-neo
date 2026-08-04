/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from 'vitest'
import {
  CommissioningFlow,
  DISCOVERY_CAP_BLE,
  DISCOVERY_CAP_ON_NETWORK,
  generateManualPairingCode,
  generateQrPayload,
} from './setup-payload'

// Canonical Matter test device: discriminator 3840, passcode 20202021,
// Standard flow. QR/onboarding values are published in connectedhomeip.
const CANONICAL = {
  passcode: 20202021,
  discriminator: 3840,
  vendorId: 0xfff1,
  productId: 0x8001,
  commissioningFlow: CommissioningFlow.Standard,
}

describe('setup-payload QR', () => {
  it('matches the canonical QR (on-network discovery)', () => {
    expect(
      generateQrPayload({ ...CANONICAL, discoveryCapabilities: DISCOVERY_CAP_ON_NETWORK }),
    ).toBe('MT:-24J0AFN00KA0648G00')
  })

  it('matches the canonical QR for vid/pid 0/0 (on-network)', () => {
    expect(
      generateQrPayload({
        ...CANONICAL,
        vendorId: 0,
        productId: 0,
        discoveryCapabilities: DISCOVERY_CAP_ON_NETWORK,
      }),
    ).toBe('MT:00000CQM00KA0648G00')
  })

  it('changes with discovery capability (BLE)', () => {
    const qr = generateQrPayload({ ...CANONICAL, discoveryCapabilities: DISCOVERY_CAP_BLE })
    expect(qr.startsWith('MT:')).toBe(true)
    expect(qr).not.toBe('MT:-24J0AFN00KA0648G00')
  })
})

describe('setup-payload manual code', () => {
  it('matches the canonical 11-digit manual code', () => {
    expect(generateManualPairingCode(CANONICAL)).toBe('34970112332')
  })

  it('is independent of vid/pid in Standard flow', () => {
    expect(generateManualPairingCode({ ...CANONICAL, vendorId: 0, productId: 0 })).toBe(
      '34970112332',
    )
  })
})
