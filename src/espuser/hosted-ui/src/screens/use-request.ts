/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useState } from 'preact/hooks'
import type { Result } from '../api/client.ts'
import { endsSignIn, type ShownError } from '../api/errors.ts'

/** useRequest runs a screen's calls one at a time: an error that ends the sign-in goes to onEnded when there is one, any other is shown with focus on `fieldToFix`. */
export function useRequest(onEnded?: (error: ShownError) => void, fieldToFix?: string) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ShownError | null>(null)

  // The disabled fieldset drops focus during a request.
  useEffect(() => {
    if (error && fieldToFix) document.getElementById(fieldToFix)?.focus()
  }, [error])

  /** run resolves true when the call worked and the page stays; a success naming where to go is followed with the screen left busy, so nothing is sent twice while the browser leaves. */
  async function run(call: () => Promise<Result>): Promise<boolean> {
    if (busy) return false
    setBusy(true)
    setError(null)
    const result = await call()
    if (result.ok && result.data.redirect_to) {
      window.location.assign(result.data.redirect_to)
      return false
    }
    setBusy(false)
    if (result.ok) return true
    if (onEnded && endsSignIn(result.error)) onEnded(result.error)
    else setError(result.error)
    return false
  }

  return { busy, error, run, clearError: () => setError(null) }
}
