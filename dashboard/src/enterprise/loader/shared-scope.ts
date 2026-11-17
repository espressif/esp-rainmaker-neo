/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import * as react from 'react'
import * as reactJsxRuntime from 'react/jsx-runtime'
import * as reactDom from 'react-dom'
import * as reactQuery from '@tanstack/react-query'
import * as reactRouter from '@tanstack/react-router'
import * as zustand from 'zustand'
import * as i18next from 'i18next'
import * as reactI18next from 'react-i18next'
import * as zod from 'zod'
import * as reactHookForm from 'react-hook-form'
import * as lucideReact from 'lucide-react'
import * as framerMotion from 'framer-motion'
import * as reactErrorBoundary from 'react-error-boundary'
import * as espdComponents from '@espressif/dashboard-ui-components/components'
import * as espdLayouts from '@espressif/dashboard-ui-components/layouts'
import * as espdCommon from '@espressif/dashboard-ui-components/common'
import { hostServices } from './host-services'

/**
 * The host half of the plugin dependency contract: plugin bundles are built with these packages
 * external, each import rewritten to a dotted lookup on `globalThis.__RMNG_SHARED__` (e.g.
 * `react` → `__RMNG_SHARED__.react`). Registering the host's own module instances is what makes
 * hooks, contexts and stores work across the boundary — a second React copy would break them all.
 *
 * Keys are identifier-safe on purpose (subpaths like
 * `@espressif/dashboard-ui-components/components` become `espdComponents`) so the generated
 * plugin code stays purely dotted. The key set is versioned by HOST_SDK_VERSION: removing or
 * renaming a key is a breaking SDK change.
 */
const sharedScope: Record<string, unknown> = {
  react,
  reactJsxRuntime,
  reactDom,
  reactQuery,
  reactRouter,
  zustand,
  i18next,
  reactI18next,
  zod,
  reactHookForm,
  lucideReact,
  framerMotion,
  reactErrorBoundary,
  espdComponents,
  espdLayouts,
  espdCommon,
  host: hostServices,
}

/** Shared-package contract version; plugins declare a semver range against it. */
export const HOST_SDK_VERSION = '1.1.0'

export const SHARED_MODULE_KEYS: readonly string[] = Object.keys(sharedScope)

export function installSharedScope(): void {
  globalThis.__RMNG_SHARED__ = sharedScope
}
