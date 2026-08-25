/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it, vi } from 'vitest'
import {
  collectExtraCapabilities,
  extraCapabilityDescriptionKey,
  extraCapabilityLabelKey,
} from './extra-capabilities'

const bridge = {
  id: 'bridge',
  labelFallback: 'Bridge',
  descriptionFallback: 'Enable bridge behavior for gateway nodes.',
  icon: 'network',
}

describe('collectExtraCapabilities', () => {
  it('collects valid capabilities across stacks', () => {
    const other = {
      id: 'acc',
      labelKey: 'enterprise-acc:capabilities.acc.label',
      labelFallback: 'ACC',
      icon: '',
    }
    const result = collectExtraCapabilities({
      'rmng-bridge-core': { DashboardCapabilityBridge: JSON.stringify(bridge), RegisterHookLambdaName: 'x' },
      'rmng-acc-core': { DashboardCapabilityAcc: JSON.stringify(other) },
      'rmng-base': { ApiGatewayUrl: 'https://example.test' },
    })
    expect(result).toEqual([bridge, other])
  })

  it('returns empty for a community deployment', () => {
    expect(collectExtraCapabilities({ 'rmng-base': { ApiGatewayUrl: 'x' } })).toEqual([])
    expect(collectExtraCapabilities({})).toEqual([])
  })

  it('skips malformed JSON loudly without throwing', () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const result = collectExtraCapabilities({
      broken: { DashboardCapabilityBad: '{not json' },
      ok: { DashboardCapabilityBridge: JSON.stringify(bridge) },
    })
    expect(result).toEqual([bridge])
    expect(error).toHaveBeenCalledOnce()
    error.mockRestore()
  })

  it('skips schema-invalid entries', () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const result = collectExtraCapabilities({
      s: {
        DashboardCapabilityNoLabel: JSON.stringify({ id: 'x', icon: '' }),
        DashboardCapabilityBadId: JSON.stringify({ ...bridge, id: 'Not Valid!' }),
      },
    })
    expect(result).toEqual([])
    expect(error).toHaveBeenCalledTimes(2)
    error.mockRestore()
  })

  it('derives conventional i18n keys, honoring explicit overrides', () => {
    expect(extraCapabilityLabelKey(bridge)).toBe(
      'enterprise-bridge:capabilities.bridge.label',
    )
    expect(extraCapabilityDescriptionKey(bridge)).toBe(
      'enterprise-bridge:capabilities.bridge.description',
    )
    const overridden = {
      ...bridge,
      labelKey: 'enterprise-acc:custom.label',
      descriptionKey: 'enterprise-acc:custom.description',
    }
    expect(extraCapabilityLabelKey(overridden)).toBe('enterprise-acc:custom.label')
    expect(extraCapabilityDescriptionKey(overridden)).toBe('enterprise-acc:custom.description')
  })

  it('dedupes by id, first stack wins', () => {
    const second = { ...bridge, labelFallback: 'Bridge (second)' }
    const result = collectExtraCapabilities({
      'a-stack': { DashboardCapabilityBridge: JSON.stringify(bridge) },
      'b-stack': { DashboardCapabilityBridge: JSON.stringify(second) },
    })
    expect(result).toEqual([bridge])
  })

  it('ignores non-string and nested-region values', () => {
    const result = collectExtraCapabilities({
      'rmng-alexa-core-us-east-1': { regions: { 'na': { AlexaSkillFunctionArn: 'arn' } } },
      weird: { DashboardCapabilityObj: { id: 'bridge' } as unknown as string },
      missing: undefined,
    })
    expect(result).toEqual([])
  })
})
