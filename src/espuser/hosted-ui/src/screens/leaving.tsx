/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useState } from 'preact/hooks'
import { destinationName, type Option } from '../lib/select-screen.ts'
import { ProviderMark } from '../components/sign-in-choices.tsx'

/** If the provider's page never arrives, such as when the person stops the load, the choices come back rather than staying locked. */
const LEAVING_TIMEOUT_MS = 15_000

interface Leaving {
  /** The redirect the page is handing over to; null while the choices are shown. */
  option: Option | null
  /** The height the card body had, so swapping in the redirect screen does not make the page jump. */
  height: number
  /** The last redirect timed out, so the choices say to try again. */
  stalled: boolean
  leave: (option: Option, height: number) => void
}

/** useLeaving tracks a redirect under way; the back button restoring this page from its cache, or the timeout, brings the choices back. */
export function useLeaving(): Leaving {
  const [state, setState] = useState<{ option: Option | null; height: number; stalled: boolean }>({ option: null, height: 0, stalled: false })
  useEffect(() => {
    if (!state.option) return
    const restored = (e: PageTransitionEvent) => { if (e.persisted) setState({ option: null, height: 0, stalled: false }) }
    window.addEventListener('pageshow', restored)
    const timer = window.setTimeout(() => setState({ option: null, height: 0, stalled: true }), LEAVING_TIMEOUT_MS)
    return () => { window.removeEventListener('pageshow', restored); window.clearTimeout(timer) }
  }, [state.option])
  return { ...state, leave: (option, height) => setState({ option, height, stalled: false }) }
}

/** The card body while the browser loads the provider's page: where it is going, and that it is on its way. */
export function LeavingScreen({ option, height }: { option: Option; height: number }) {
  return (
    // Capped at the viewport, so on a phone a tall form's height never pushes the message below the fold.
    <div class="panel leaving" role="status" style={{ minHeight: `min(${height}px, 70vh)` }}>
      <span class="leaving-mark" aria-hidden="true"><ProviderMark option={option} /></span>
      <h1>Taking you to {destinationName(option)}</h1>
      <p class="leaving-hint">This may take a moment.</p>
    </div>
  )
}
