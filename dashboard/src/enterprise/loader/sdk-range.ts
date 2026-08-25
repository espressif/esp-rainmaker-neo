/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Minimal semver-satisfies for the SDK ranges plugins declare: `^major[.minor[.patch]]` or an
 * exact version. Enough for the lockstep contract; not a general semver implementation.
 */
export function sdkRangeSatisfied(range: string, version: string): boolean {
  const caret = range.startsWith('^')
  const want = (caret ? range.slice(1) : range).split('.').map(Number)
  const have = version.split('.').map(Number)
  if (want.some(Number.isNaN) || have.some(Number.isNaN)) {
    return false
  }
  if (!caret) {
    return want.every((part, i) => part === (have[i] ?? 0))
  }
  if (have[0] !== want[0]) {
    return false
  }
  for (let i = 1; i < want.length; i++) {
    const haveAtDepth = have[i] ?? 0
    const wantAtDepth = want[i]
    if (haveAtDepth !== wantAtDepth) {
      return haveAtDepth > wantAtDepth
    }
  }
  return true
}
