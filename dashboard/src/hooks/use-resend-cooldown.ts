/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useEffect, useState } from "react";

const DEFAULT_COOLDOWN_SECONDS = 30;

/**
 * Cooldown for "resend the code" controls, shared by the sign-in OTP screen and
 * the reset-password screen. Prevents accidental double-sends: the control stays
 * disabled with a countdown while a just-sent code is still in flight to the inbox.
 *
 * @param startArmed arm the cooldown on mount — pass `true` where arrival implies a
 * code was just sent, `false` where the admin may already hold one.
 */
export function useResendCooldown(
  startArmed: boolean,
  seconds: number = DEFAULT_COOLDOWN_SECONDS,
) {
  const [deadline, setDeadline] = useState(() =>
    startArmed ? Date.now() + seconds * 1000 : 0,
  );
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (deadline <= Date.now()) {
      return;
    }
    // Sub-second tick so the first rendered value never looks a second stale;
    // the interval dies with the deadline instead of running forever.
    const id = setInterval(() => {
      setNow(Date.now());
      if (Date.now() >= deadline) {
        clearInterval(id);
      }
    }, 250);
    return () => clearInterval(id);
  }, [deadline]);

  const restart = useCallback(() => {
    setDeadline(Date.now() + seconds * 1000);
    setNow(Date.now());
  }, [seconds]);

  const secondsLeft = Math.max(0, Math.ceil((deadline - now) / 1000));

  return { secondsLeft, isCoolingDown: secondsLeft > 0, restart };
}
