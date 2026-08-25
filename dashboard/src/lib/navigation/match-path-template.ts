/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Matches a path template (with `$paramName` tokens) against a real pathname
 * and extracts param values. Returns null when the shape doesn't match.
 *
 * Lives outside `app/router.tsx` so non-router code (the enterprise plugin
 * outlet) can match manifest route templates without importing the router
 * module and its route-tree side effects.
 */
export function matchPathTemplate(
  template: string,
  pathname: string,
): Record<string, string> | null {
  const tSeg = template.split('/').filter(Boolean)
  const pSeg = pathname.split('/').filter(Boolean)
  if (tSeg.length !== pSeg.length) {return null}
  const params: Record<string, string> = {}
  for (let i = 0; i < tSeg.length; i++) {
    const token = tSeg[i]
    if (token.startsWith('$')) {
      params[token.slice(1)] = decodeURIComponent(pSeg[i])
    } else if (token !== pSeg[i]) {
      return null
    }
  }
  return params
}
