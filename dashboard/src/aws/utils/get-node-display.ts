/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { extractIparamsFields } from './iparams-fields'

export interface NodeDisplay {
  nodeId: string
  displayName: string | null
}

export function getNodeDisplay(thing: {
  thingName?: string
  shadow?: string | null
}): NodeDisplay {
  const nodeId = thing.thingName ?? ''
  const displayName = extractIparamsFields(thing.shadow ?? undefined).displayName
  return { nodeId, displayName }
}
