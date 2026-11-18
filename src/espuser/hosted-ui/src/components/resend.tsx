/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useState } from 'preact/hooks'

// The server makes a person wait this many seconds between two sends of a code.
const COOLDOWN = 30

/** ResendLink counts down from the send that brought it on screen and again after each resend that worked, so it is never offered before the server would accept it. */
export function ResendLink({ onResend }: { onResend: () => Promise<boolean> }) {
  const [left, setLeft] = useState(COOLDOWN)
  useEffect(() => {
    if (left <= 0) return
    const id = setTimeout(() => setLeft(n => n - 1), 1000)
    return () => clearTimeout(id)
  }, [left])

  return (
    <div class="form-links form-links--single">
      <button type="button" class="link" disabled={left > 0} onClick={async () => { if (await onResend()) setLeft(COOLDOWN) }}>
        {left > 0 ? `Resend code in 0:${String(left).padStart(2, '0')}` : 'Resend code'}
      </button>
    </div>
  )
}
