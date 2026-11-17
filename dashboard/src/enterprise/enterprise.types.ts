/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from 'react'
import type { ManifestFeature } from './loader/manifest.schema'

/** All runtime-contributed enterprise routes live under this prefix (spec naming freeze). */
export const ENTERPRISE_ROUTE_PREFIX = '/home/ent/'

/** i18next namespace a plugin's bundles are registered under. */
export function enterpriseNamespace(featureId: string): string {
  return `enterprise-${featureId}`
}

/**
 * Props a registered component may receive: route components get none; extension-slot
 * components get the slot's `context` (its per-slot shape is the SDK's typed contract —
 * plugin components narrow it there).
 */
export interface EnterpriseComponentProps {
  context?: Record<string, unknown>
}

export type EnterpriseComponent = ComponentType<EnterpriseComponentProps>

/**
 * What a plugin bundle hands to `globalThis.__RMNG_ENTERPRISE_REGISTER__` when imported.
 * `contributes` is honoured only by the DEV `?enterpriseDevUrl=` escape hatch — in production
 * contributions come exclusively from the plugin index.
 */
export interface RegisteredPlugin {
  id: string
  version?: string
  sdk?: string
  components: Record<string, EnterpriseComponent>
  i18n?: Record<string, Record<string, unknown>>
  contributes?: ManifestFeature['contributes']
}

declare global {
  var __RMNG_SHARED__: Record<string, unknown> | undefined
  var __RMNG_ENTERPRISE_REGISTER__: ((plugin: RegisteredPlugin) => void) | undefined
}
