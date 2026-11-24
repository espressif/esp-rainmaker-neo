/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from 'preact/hooks'
import { api } from '../api/client.ts'
import { AccountChip } from '../components/account-chip.tsx'
import { Assist } from '../components/inputs.tsx'
import { useRequest } from './use-request.ts'

/** Where a sign-out ends when there is no app to return to, or a Cancel. */
export function SignOutResult({ outcome }: { outcome: 'signed-out' | 'kept' }) {
  return (
    <div class="panel terminal">
      <h1>{outcome === 'signed-out' ? 'You’re signed out' : 'You’re still signed in'}</h1>
      <p class="terminal-hint">You can close this page.</p>
    </div>
  )
}

/** The sign-out question: any site can send a browser to /oauth2/logout, so only the person's own Sign out ends the session. */
export function LogoutScreen({ username, csrfToken }: { username?: string; csrfToken: string }) {
  // No sign-in to end here, so every error is shown on the screen.
  const { busy, error, run } = useRequest()
  const [ended, setEnded] = useState<'signed-out' | 'kept' | null>(null)

  async function signOut() {
    if (await run(() => api.confirmLogout(csrfToken))) setEnded('signed-out')
  }

  // Never the app's post-logout page, which would tell the app a sign-out happened, nor history.back(), which can replay an app's old callback URL.
  function cancel() {
    setEnded('kept')
  }

  if (ended) return <SignOutResult outcome={ended} />

  return (
    <div class="panel consent logout">
      <h1 class="consent-title">Sign out?</h1>
      <AccountChip username={username} />

      <Assist error={error} />
      <div class="consent-actions">
        <button type="button" class="secondary" disabled={busy} onClick={cancel}>Cancel</button>
        <button type="button" class="primary" disabled={busy} onClick={signOut}>
          {busy ? 'Signing out…' : 'Sign out'}
        </button>
      </div>
    </div>
  )
}
