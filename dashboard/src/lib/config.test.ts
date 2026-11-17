/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { beforeEach, describe, expect, it } from 'vitest'
import { useConfigStore } from '@/stores/config.store'
import { getVoiceAssistantClientId } from './config'

describe('getVoiceAssistantClientId', () => {
  beforeEach(() => useConfigStore.getState().clearConfig())

  it("names each assistant's own client, so its tab shows the credentials its console needs", () => {
    useConfigStore.getState().setConfig({
      ALEXA_CLIENT_ID: 'rainmaker-neo-alexa-client',
      GVA_CLIENT_ID: 'rainmaker-neo-gva-client',
      SMARTTHINGS_CLIENT_ID: 'rainmaker-neo-smartthings-client',
    })
    expect(getVoiceAssistantClientId('alexa')).toBe('rainmaker-neo-alexa-client')
    expect(getVoiceAssistantClientId('gva')).toBe('rainmaker-neo-gva-client')
    expect(getVoiceAssistantClientId('smartthings')).toBe('rainmaker-neo-smartthings-client')
  })

  it('is empty before any config is loaded', () => {
    expect(getVoiceAssistantClientId('gva')).toBe('')
  })
})
