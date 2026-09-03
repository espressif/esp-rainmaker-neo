/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Button } from "@espressif/dashboard-ui-components/components";
import type { ResendCodeHintProps } from "./resend-code-hint.props";

/**
 * "Resend code" control rendered in an `InputOTP`'s hint slot, shared by the
 * sign-in OTP screen and the reset-password screen. Shows a countdown while the
 * cooldown holds, then the live resend action.
 */
export default function ResendCodeHint({
  isCoolingDown,
  countdownLabel,
  resendLabel,
  isResending,
  onResend,
}: ResendCodeHintProps) {
  if (isCoolingDown) {
    return (
      <span className="text-sm text-muted-foreground tabular-nums">
        {countdownLabel}
      </span>
    );
  }

  return (
    <Button
      type="button"
      variant="link"
      className="p-0 h-auto"
      loading={isResending}
      onClick={onResend}
    >
      {resendLabel}
    </Button>
  );
}
