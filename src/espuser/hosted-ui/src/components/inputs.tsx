/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useRef, useState } from 'preact/hooks'
import type { ShownError } from '../api/errors.ts'
import { AlertMark, EyeMark, WarnMark } from './marks.tsx'

export const CODE_LENGTH = 6

/** Assist is the line under a field: the error, or nothing. The alert stays mounted, since screen readers announce text that appears in one more reliably than a new one. */
export function Assist({ id, error }: { id?: string; error: ShownError | null }) {
  return (
    <div id={id} class={error ? `assist assist--${error.severity}` : 'assist'} role="alert">
      {error ? (
        <>
          {error.severity === 'error' ? <AlertMark /> : <WarnMark />}
          <span>{error.message}</span>
        </>
      ) : null}
    </div>
  )
}

export function BackLine({ prompt, action = 'Sign in', onBack }: { prompt: string; action?: string; onBack: () => void }) {
  return <p class="back-line">{prompt} <button type="button" class="link" onClick={onBack}>{action}</button></p>
}

/** `describedBy` names the Assist that reports on this field, so a screen reader reads the error with it. */
export function UsernameField({ id, value, onInput, invalid, describedBy }: {
  id: string; value: string; onInput: (v: string) => void; invalid?: boolean; describedBy?: string
}) {
  return (
    <div class="field">
      <label for={id}>Email or phone number</label>
      <input
        id={id} type="text" value={value} autoComplete="username" inputMode="email" placeholder="name@example.com"
        aria-invalid={invalid ? 'true' : undefined} aria-describedby={describedBy}
        onInput={e => onInput(e.currentTarget.value)}
      />
    </div>
  )
}

/** Password field with a reveal toggle and a Caps Lock warning; `choosing` asks for a new password, so managers offer to generate one and the pool's rules show. */
export function PasswordField({ id, value, onInput, invalid, label = 'Password', choosing = false, describedBy }: {
  id: string; value: string; onInput: (v: string) => void; invalid?: boolean; label?: string; choosing?: boolean; describedBy?: string
}) {
  const [shown, setShown] = useState(false)
  const [caps, setCaps] = useState(false)
  const described = [choosing ? `${id}-hint` : '', describedBy ?? ''].filter(Boolean).join(' ')

  return (
    <div class="field">
      <label for={id}>{label}</label>
      <div class="control">
        <input
          id={id} type={shown ? 'text' : 'password'} value={value} autoComplete={choosing ? 'new-password' : 'current-password'}
          aria-invalid={invalid ? 'true' : undefined} aria-describedby={described || undefined}
          onInput={e => onInput(e.currentTarget.value)}
          // Optional: Chrome's autofill fires a keyup that is not a KeyboardEvent.
          onKeyUp={e => setCaps(e.getModifierState?.('CapsLock') ?? false)}
        />
        {/* Inside the field so the warning never moves the text being typed. */}
        {caps ? (
          <span class="caps" role="status">
            <span aria-hidden="true">⇪</span>
            <span class="caps-text">Caps Lock</span>
          </span>
        ) : null}
        {/* One name; aria-pressed carries the state, so it never reads as "Hide password, pressed". */}
        <button type="button" class="reveal" onClick={() => setShown(s => !s)} aria-label="Show password" aria-pressed={shown}>
          <EyeMark off={shown} />
        </button>
      </div>
      {/* rmng's own pool rules (cdk/utils/app_common.py), also Cognito's defaults; only the pool accepts or refuses, so nothing here rates a password. */}
      {choosing ? <p id={`${id}-hint`} class="password-rules">At least 8 characters, with upper and lower case letters, a number and a symbol.</p> : null}
    </div>
  )
}

/** One field for the whole code, so a phone's autofill or a paste lands in it whole; it takes focus whenever it is empty, on arrival and after a wrong code clears it. */
export function CodeField({ id, label, value, onChange, onComplete, invalid, describedBy }: {
  id: string; label: string; value: string; onChange: (code: string) => void
  /** Called once every digit is in, for a screen that submits without a button. */
  onComplete?: (code: string) => void
  invalid?: boolean; describedBy?: string
}) {
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (value === '') input.current?.focus()
  }, [value])

  return (
    <div class="field code">
      <label for={id}>{label}</label>
      <input
        ref={input} id={id} type="text" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]*"
        value={value} aria-invalid={invalid ? 'true' : undefined} aria-describedby={describedBy}
        onInput={e => {
          // No maxLength: it would cut a pasted "123 456" to "123 45" before the separators are dropped.
          const digits = e.currentTarget.value.replace(/\D/g, '').slice(0, CODE_LENGTH)
          // Set directly: a rejected letter leaves the state unchanged, so no render would remove it.
          e.currentTarget.value = digits
          onChange(digits)
          if (digits.length === CODE_LENGTH) onComplete?.(digits)
        }}
      />
    </div>
  )
}
