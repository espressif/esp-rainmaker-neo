/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { PageData } from '../page-data.ts'

const LABELS = { support: 'Support', privacy: 'Privacy', terms: 'Terms' } as const

/** The line under the card; only links the deployment configured are drawn, so none is ever empty. */
export function PageFooter({ links }: { links: PageData['links'] }) {
  const named = (Object.keys(LABELS) as (keyof typeof LABELS)[]).filter(key => links?.[key])
  return (
    <footer class="page-foot">
      <span>© {new Date().getFullYear()} Espressif Systems</span>
      {named.length ? (
        <nav aria-label="Legal and support">
          {/* A new tab, so following a link never abandons the sign-in in progress. */}
          {named.map(key => <a key={key} href={links![key]} target="_blank" rel="noopener noreferrer">{LABELS[key]}</a>)}
        </nav>
      ) : null}
    </footer>
  )
}
