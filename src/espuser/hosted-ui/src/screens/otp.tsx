/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from 'preact/hooks'
import { api } from '../api/client.ts'
import type { ShownError } from '../api/errors.ts'
import { Assist, BackLine, CodeField, UsernameField } from '../components/inputs.tsx'
import { ResendLink } from '../components/resend.tsx'
import { useRequest } from './use-request.ts'

/** Masked so the screen confirms which address was used without reprinting it in full: "someone@example.com" -> "s•••@example.com". */
function mask(address: string): string {
  const [user, domain] = address.split('@')
  if (!user || !domain) return address
  return `${user.slice(0, 1)}•••@${domain}`
}

/** Code sign-in takes the whole card, so a two-step flow is not interrupted by a mis-click. */
export function OTPScreen({ username, onUsernameChange, flowID, onBack, onEnded }: {
  username: string
  onUsernameChange: (value: string) => void
  flowID?: string
  onBack: () => void
  onEnded: (error: ShownError) => void
}) {
  const [sentTo, setSentTo] = useState<string | null>(null)
  const [code, setCode] = useState('')
  const { busy, error, run, clearError } = useRequest(onEnded, sentTo ? 'otp-code' : 'otp-username')

  async function send(address: string): Promise<boolean> {
    const sent = await run(() => api.sendCode(address, flowID))
    if (sent) {
      setSentTo(address.trim())
      setCode('')
    }
    return sent
  }

  // A wrong code is cleared so the next one starts from the first digit.
  const verify = (entered: string) => run(async () => {
    const result = await api.verifyCode(entered, flowID)
    if (!result.ok) setCode('')
    return result
  })

  const back = <BackLine prompt="Prefer another way?" action="All sign-in options" onBack={onBack} />

  if (!sentTo) {
    return (
      <form class="panel" onSubmit={e => { e.preventDefault(); send(username) }} noValidate>
        <fieldset disabled={busy}>
          <UsernameField id="otp-username" value={username} onInput={onUsernameChange} invalid={!!error} describedBy="otp-error" />
          <Assist id="otp-error" error={error} />
          <button type="submit" class="primary" disabled={busy || !username.trim()}>
            {busy ? 'Sending…' : 'Send code'}
          </button>
          {back}
        </fieldset>
      </form>
    )
  }

  return (
    <div class="panel">
      <fieldset disabled={busy}>
        <p class="lede">
          Code sent to
          <span class="address">
            <strong>{mask(sentTo)}</strong>{' '}
            <button type="button" class="link" onClick={() => { setSentTo(null); clearError() }}>Change</button>
          </span>
        </p>
        <CodeField id="otp-code" label="Code" value={code} onChange={setCode} onComplete={verify} invalid={!!error} describedBy="otp-error" />
        <Assist id="otp-error" error={error} />
        <ResendLink onResend={() => send(sentTo)} />
        {back}
      </fieldset>
    </div>
  )
}
