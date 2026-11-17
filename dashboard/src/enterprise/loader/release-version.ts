/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** What a stack or a plugin build reports when it could not read the release version. */
const UNKNOWN_VERSION = 'unknown'

export interface ReleaseMismatch {
  stack: string
  stackVersion: string
}

/**
 * First backend stack a feature requires whose release differs from the plugins' own: `(['rmng-acc-core'], { 'rmng-acc-core': '0.8.0' }, '0.8.1')` -> `{ stack: 'rmng-acc-core', stackVersion: '0.8.0' }`. A side that reports no version (core stacks, or 'unknown') is not compared, so the result is then null.
 */
export function findReleaseMismatch(
  requiredStacks: readonly string[],
  stackVersions: Record<string, string>,
  pluginsVersion: string | undefined,
): ReleaseMismatch | null {
  if (!pluginsVersion || pluginsVersion === UNKNOWN_VERSION) {
    return null
  }
  for (const stack of requiredStacks) {
    const stackVersion = stackVersions[stack]
    if (stackVersion && stackVersion !== UNKNOWN_VERSION && stackVersion !== pluginsVersion) {
      return { stack, stackVersion }
    }
  }
  return null
}
