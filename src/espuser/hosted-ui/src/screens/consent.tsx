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
import { describeScopes, OFFLINE_ACCESS, stayConnected, type Grant } from '../lib/scopes.ts'
import { useRequest } from './use-request.ts'

export function ConsentScreen({ appName, logoURI, username, scopes, flowID, onEnded }: {
  appName: string
  logoURI?: string
  username?: string
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
      <AccountChip username={username} />

      <ul class="grants">
        {describeScopes(scopes).map(grant => (
          <li key={grant.label}>
            <span class="grant-label">{grant.label}</span>
            {grant.detail ? <span class="grant-detail">{grant.detail}</span> : null}
          </li>
        ))}
        {scopes.includes(OFFLINE_ACCESS) ? <StayConnectedRow grant={stayConnected(appName)} /> : null}
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

/** The list's closing row for offline_access, tinted apart from the data rows above it. */
function StayConnectedRow({ grant }: { grant: Grant }) {
  return (
    <li class="grant-stay-connected">
      <span class="grant-stay-connected-text">
        <span class="grant-label">{grant.label}</span>
        <span class="grant-detail">{grant.detail}</span>
      </span>
      <span class="grant-stay-connected-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" strokeWidth="2.1"
             strokeLinecap="round" strokeLinejoin="round">
          <path d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07l-1.5 1.5"/>
          <path d="M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07l1.5-1.5"/>
        </svg>
      </span>
    </li>
  )
}
