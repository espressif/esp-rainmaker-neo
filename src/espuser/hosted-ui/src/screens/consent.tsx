/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from 'preact/hooks'
import { api } from '../api/client.ts'
import type { ShownError } from '../api/errors.ts'
import { AccountChip } from '../components/account-chip.tsx'
import { Assist } from '../components/inputs.tsx'
import { AppMark, EspressifMark } from '../components/marks.tsx'
import { describeScopes } from '../lib/scopes.ts'
import { useRequest } from './use-request.ts'

export function ConsentScreen({ appName, logoURI, account, scopes, flowID, onEnded }: {
  appName: string
  logoURI?: string
  account?: string
  scopes: string[]
  flowID?: string
  onEnded: (error: ShownError) => void
}) {
  const { busy, error, run } = useRequest(onEnded)
  const [decision, setDecision] = useState<'allow' | 'deny'>('allow')

  function answer(choice: 'allow' | 'deny') {
    setDecision(choice)
    run(() => api.answerConsent(choice, flowID))
  }

  return (
    <div class="panel consent">
      <div class="consent-parties" aria-hidden="true">
        <span class="consent-tile">
          {/* Only an inline image: the registry admits nothing else, and the page's CSP would block anything remote. */}
          {logoURI?.startsWith('data:image/') ? <img src={logoURI} alt="" /> : <AppMark />}
        </span>
        <svg class="consent-link" viewBox="0 0 32 16" width="32" height="16" fill="none" stroke="currentColor"
             strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M3 5h26l-4-3.5M29 11H3l4 3.5"/>
        </svg>
        <span class="consent-tile"><EspressifMark /></span>
      </div>

      <h1 class="consent-title">Authorize app</h1>
      <p class="consent-lede"><strong>{appName}</strong> wants to access your Espressif account.</p>
      <AccountChip account={account} />

      <ul class="grants">
        {describeScopes(scopes).map(grant => (
          <li key={grant.label}>
            <span class="grant-label">{grant.label}</span>
            {grant.detail ? <span class="grant-detail">{grant.detail}</span> : null}
          </li>
        ))}
      </ul>

      <Assist error={error} />
      <div class="consent-actions">
        <button type="button" class="secondary" disabled={busy} onClick={() => answer('deny')}>
          {busy && decision === 'deny' ? 'Cancelling…' : 'Cancel'}
        </button>
        <button type="button" class="primary" disabled={busy} onClick={() => answer('allow')}>
          {busy && decision === 'allow' ? 'Allowing…' : 'Allow'}
        </button>
      </div>
    </div>
  )
}
