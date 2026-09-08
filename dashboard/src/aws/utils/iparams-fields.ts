/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

const EMPTY_IPARAMS_FIELDS = {
  online: null,
  deviceType: null,
  deviceModel: null,
  fwVersion: null,
  lastSeen: null,
  displayName: null,
} as const

export type IparamsFields = {
  online: boolean | null
  deviceType: string | null
  deviceModel: string | null
  fwVersion: string | null
  lastSeen: number | null
  displayName: string | null
}

function readStringField(value: unknown): string | null {
  return typeof value === 'string' ? value : null
}

function readBooleanField(value: unknown): boolean | null {
  return typeof value === 'boolean' ? value : null
}

function readTimestamp(value: unknown): number | null {
  if (!value || typeof value !== 'object') {
    return null
  }
  const timestamp = (value as { timestamp?: unknown }).timestamp
  return typeof timestamp === 'number' ? timestamp : null
}

function readDeviceFields(reported: Record<string, unknown> | undefined) {
  const device = (reported?.data as { device?: { t?: Record<string, unknown> } } | undefined)?.device?.t
  return {
    displayName: readStringField(device?.name),
    deviceType: readStringField(device?.type),
    deviceModel: readStringField(device?.model),
    fwVersion: readStringField(device?.fw_version),
  }
}

/**
 * Fall back to the disconnect timestamp when the shadow has never carried an
 * `online` metadata timestamp — e.g. a node that has only ever gone offline
 * still tells us when it did so. Values are in milliseconds here, so we scale
 * them down to match the seconds-based `metadata.reported.online.timestamp`
 * that every downstream caller expects.
 */
function readLastSeen(
  reported: Record<string, unknown> | undefined,
  metadataReported: Record<string, unknown> | undefined,
): number | null {
  const metadataTs = readTimestamp(metadataReported?.online)
  if (metadataTs != null) {
    return metadataTs
  }
  const disconnectInfo = reported?.disconnect_info as
    | { last_disconnect_ts?: unknown }
    | undefined
  const disconnectTs = disconnectInfo?.last_disconnect_ts
  if (typeof disconnectTs === 'number') {
    return Math.floor(disconnectTs / 1000)
  }
  return null
}

function extractFromParsedShadow(parsed: unknown): IparamsFields {
  if (!parsed || typeof parsed !== 'object') {
    return { ...EMPTY_IPARAMS_FIELDS }
  }
  const iparams = (parsed as { name?: { iparams?: Record<string, unknown> } }).name?.iparams
  const reported = iparams?.reported as Record<string, unknown> | undefined
  const metadata = (iparams?.metadata as { reported?: Record<string, unknown> } | undefined)?.reported
  const deviceFields = readDeviceFields(reported)

  return {
    online: readBooleanField(reported?.online),
    deviceType: deviceFields.deviceType,
    deviceModel: deviceFields.deviceModel,
    fwVersion: deviceFields.fwVersion,
    lastSeen: readLastSeen(reported, metadata),
    displayName: deviceFields.displayName,
  }
}

export function extractIparamsFields(shadow: string | undefined): IparamsFields {
  if (!shadow) {
    return { ...EMPTY_IPARAMS_FIELDS }
  }
  try {
    return extractFromParsedShadow(JSON.parse(shadow))
  } catch {
    return { ...EMPTY_IPARAMS_FIELDS }
  }
}
