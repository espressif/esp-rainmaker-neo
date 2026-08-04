/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * QR code payload construction and PNG generation for provisioning.
 * Ported from esp_rainmaker_dashboard/src/utils/bulkGenerate/qrGen.js
 */

import QRCode from 'qrcode'

/**
 * Build the provisioning QR code payload string in compact format.
 * Format: NP:<name>|<pop>|<transport_char>
 * where transport_char is 'b' for BLE and 's' for SoftAP.
 */
export function buildQrPayload(randomHex: string): string {
  const pop = randomHex.substring(0, 8)
  const provName = 'PROV_' + randomHex.substring(randomHex.length - 6)
  return `NP:${provName}|${pop}|b`
}

/**
 * Generate a QR code PNG as a Uint8Array.
 */
export async function generateQrPng(text: string): Promise<Uint8Array> {
  const dataUrl = await QRCode.toDataURL(text, { type: 'image/png', width: 300, margin: 2 })
  const b64 = dataUrl.split(',')[1]
  const binary = atob(b64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) {bytes[i] = binary.charCodeAt(i)}
  return bytes
}
