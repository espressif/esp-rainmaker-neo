/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useRef, useState } from 'preact/hooks'
import { account } from '../api/client.ts'
import type { ShownError } from '../api/errors.ts'
import { Assist, BackLine, CODE_LENGTH, CodeField, PasswordField, UsernameField } from '../components/inputs.tsx'
import { ResendLink } from '../components/resend.tsx'
import type { PageData } from '../page-data.ts'
import { useRequest } from './use-request.ts'

export type AccountStep = 'sign-up' | 'verify' | 'forgot' | 'reset'

/** Each step's heading, button, way back and fields; `password` is the password field's label. */
export const ACCOUNT_STEPS: Record<AccountStep, { title: string; submit: string; back: string; username?: true; code?: true; password?: string }> = {
  'sign-up': { title: 'Create an account', submit: 'Create account', back: 'Already have an account?', username: true, password: 'Password' },
  // Sign-up answers an existing account with a mistyped password exactly as a new one, so this is that person's way back too.
  verify: { title: 'Verify your account', submit: 'Continue', back: 'Already have an account?', code: true },
  forgot: { title: 'Reset your password', submit: 'Continue', back: 'Remembered your password?', username: true },
  reset: { title: 'Choose a new password', submit: 'Update password', back: 'Remembered your password?', code: true, password: 'New password' },
}

/** What creating an account agrees to, naming only the pages the deployment links. */
function AgreementLine({ links }: { links?: PageData['links'] }) {
  const pages = [
    links?.terms ? <a href={links.terms} target="_blank" rel="noopener noreferrer">Terms of Service</a> : null,
    links?.privacy ? <a href={links.privacy} target="_blank" rel="noopener noreferrer">Privacy Policy</a> : null,
  ].filter(Boolean)
  if (!pages.length) return null
  return (
    <p class="agreement">
      By creating an account, you agree to the {pages[0]}{pages.length === 2 ? <> and {pages[1]}</> : null}.
    </p>
  )
}

export function AccountScreen({ step, providerName, username, onUsernameChange, onStep, onBack, onEnded, links, initialPassword = '' }: {
  step: AccountStep
  /** A password the person already typed, such as the one that proved an unverified account; signing up again with it resends the code. */
  initialPassword?: string
  providerName?: string
  links?: PageData['links']
  username: string
  onUsernameChange: (value: string) => void
  onStep: (next: AccountStep) => void
  onBack: () => void
  onEnded: (error: ShownError) => void
}) {
  const [password, setPassword] = useState(initialPassword)
  const [code, setCode] = useState('')
  const [done, setDone] = useState<string | null>(null)
  const { busy, error, run } = useRequest(onEnded)
  const form = useRef<HTMLFormElement>(null)
  const spec = ACCOUNT_STEPS[step]

  // The step's first field is where it starts and where its error is read; the disabled fieldset drops focus during a request.
  useEffect(() => {
    form.current?.querySelector('input')?.focus()
  }, [step, error])

  async function submit(e: Event) {
    e.preventDefault()
    switch (step) {
      case 'sign-up':
        if (await run(() => account.signUp(username, password, providerName))) onStep('verify')
        return
      case 'verify':
        if (await run(() => account.verifySignup(username, code, providerName))) setDone('Your account is verified. You can sign in now.')
        return
      case 'forgot':
        if (await run(() => account.startRecovery(username, providerName))) onStep('reset')
        return
      case 'reset':
        if (await run(() => account.confirmRecovery(username, code, password, providerName))) setDone('Your password is set. You can sign in now.')
    }
  }

  async function resend(): Promise<boolean> {
    const sent = await run(() => step === 'reset' ? account.startRecovery(username, providerName) : account.signUp(username, password, providerName))
    if (sent) setCode('')
    return sent
  }

  const ready =
    (!spec.username || username.trim() !== '') &&
    (!spec.code || code.length === CODE_LENGTH) &&
    (!spec.password || password !== '')

  if (done) {
    return (
      <div class="panel">
        <p class="lede">{done}</p>
        <button type="button" class="primary" onClick={onBack}>Sign in</button>
      </div>
    )
  }

  return (
    <form ref={form} class="panel" onSubmit={submit} noValidate>
      <fieldset disabled={busy}>
        {spec.username ? (
          <UsernameField id="acct-username" value={username} onInput={onUsernameChange} invalid={!!error} describedBy="account-error" />
        ) : null}

        {spec.code ? (
          <>
            <p class="lede">We sent a code to <strong class="address">{username}</strong></p>
            <CodeField id="acct-code" label="Verification code" value={code} onChange={setCode} invalid={!!error} describedBy="account-error" />
          </>
        ) : null}

        {spec.password ? (
          <PasswordField id="acct-password" label={spec.password} value={password} onInput={setPassword} invalid={!!error} describedBy="account-error" choosing />
        ) : null}

        <Assist id="account-error" error={error} />

        <button type="submit" class="primary" disabled={busy || !ready}>
          {busy ? 'Working…' : spec.submit}
        </button>

        {step === 'sign-up' ? <AgreementLine links={links} /> : null}
        {spec.code ? <ResendLink onResend={resend} /> : null}
        <BackLine prompt={spec.back} onBack={onBack} />
      </fieldset>
    </form>
  )
}
