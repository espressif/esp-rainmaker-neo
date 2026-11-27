/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { terminalHeading } from '../api/errors.ts'

/** A sign-in that cannot continue shows only the OAuth code and a sentence, never a client name or redirect uri. */
export function TerminalScreen({ code, message, restartURL }: {
  code?: string
  message: string
  restartURL?: string
}) {
  const heading = terminalHeading(code, message)
  const detail = [heading === message ? '' : message, code ?? ''].filter(Boolean).join(' · ')
  return (
    <div class="panel terminal">
      <h1>{heading}</h1>
      {restartURL ? (
        <button type="button" class="primary" onClick={() => window.location.assign(restartURL)}>Start again</button>
      ) : (
        <p class="terminal-hint">Please return to the application and try signing in again.</p>
      )}
      {detail ? <p class="terminal-code">{detail}</p> : null}
    </div>
  )
}
