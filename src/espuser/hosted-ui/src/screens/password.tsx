/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from 'preact/hooks'
import { account, api } from '../api/client.ts'
import type { ShownError } from '../api/errors.ts'
import { Assist, PasswordField, UsernameField } from '../components/inputs.tsx'
import { useRequest } from './use-request.ts'

export function PasswordScreen({ providerName, flowID, username, onUsernameChange, onEnded, onForgot, onSignUp, onUnconfirmed }: {
  providerName: string
  flowID?: string
  username: string
  onUsernameChange: (value: string) => void
  onEnded: (error: ShownError) => void
  onForgot: () => void
  onSignUp: () => void
  /** The account exists but is unverified and its code was just resent; the password goes along for the next resend. */
  onUnconfirmed: (password: string) => void
}) {
  const [password, setPassword] = useState('')
  const { busy, error, run } = useRequest(onEnded, 'password')

  const submit = (e: Event) => {
    e.preventDefault()
    run(async () => {
      const result = await api.signIn(username, password, providerName, flowID)
      if (result.ok || result.error.action !== 'send-code') return result
      // Signing up again with the password just proven is how the server resends an unverified account's code.
      const resent = await account.signUp(username, password, providerName)
      if (!resent.ok) return result
      onUnconfirmed(password)
      return resent
    })
  }

  return (
    <form class="panel" onSubmit={submit} noValidate>
      <fieldset disabled={busy}>
        <UsernameField id="username" value={username} onInput={onUsernameChange} />
        <PasswordField id="password" value={password} onInput={setPassword} invalid={!!error} describedBy="signin-error" />
        <Assist id="signin-error" error={error} />
        <button type="submit" class="primary" disabled={busy || !username.trim() || !password}>
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
        <div class="form-links">
          <button type="button" class="link" onClick={onForgot}>Forgot password?</button>
          <button type="button" class="link" onClick={onSignUp}>Create an account</button>
        </div>
      </fieldset>
    </form>
  )
}
