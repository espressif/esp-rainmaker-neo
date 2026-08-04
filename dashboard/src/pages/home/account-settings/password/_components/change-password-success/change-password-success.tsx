/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { LogIn } from "lucide-react";
import { Alert, Button } from "@espressif/dashboard-ui-components/components";
import { logout } from "@/lib/auth";

/**
 * Login lands here with the "password updated" banner already shown, so the admin
 * sees why they were signed out.
 */
const SIGN_IN_PATH = "/login?reset=success";

/** Seconds the confirmation stays on screen before the automatic sign-out. */
const SIGN_OUT_DELAY_SECONDS = 8;

/**
 * Shown in place of the form once the password has changed.
 *
 * Cognito keeps the current access token valid after a password change, so signing
 * out is a deliberate choice rather than a technical requirement: it forces the new
 * password to be used at least once and drops any session opened with the old one.
 * The countdown makes that explicit — the previous page did it silently on a 1.5s
 * timer with no explanation.
 */
export default function ChangePasswordSuccess() {
  const { t } = useTranslation("account-settings");
  const [secondsLeft, setSecondsLeft] = useState(SIGN_OUT_DELAY_SECONDS);

  const signOutNow = useCallback(() => logout(SIGN_IN_PATH), []);

  // Re-armed on every tick, and cleared on unmount so leaving the tab before the
  // countdown ends cancels the redirect instead of firing it from another page.
  useEffect(() => {
    if (secondsLeft <= 0) {
      logout(SIGN_IN_PATH);
      return;
    }

    const timer = setTimeout(() => setSecondsLeft(secondsLeft - 1), 1000);
    return () => clearTimeout(timer);
  }, [secondsLeft]);

  return (
    <div className="flex flex-col gap-4">
      <Alert
        type="success"
        variant="soft"
        color="success"
        title={t("password.success.title", "Password updated")}
        description={t(
          "password.success.description",
          "Your password has been changed. You'll be signed out so you can sign in again with the new one.",
        )}
      />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground" aria-live="polite">
          {t("password.success.countdown", {
            defaultValue: "Signing you out in {{seconds}}s.",
            seconds: secondsLeft,
          })}
        </p>

        <Button
          type="button"
          size="lg"
          fullWidth={false}
          startIcon={<LogIn className="h-4 w-4 shrink-0" aria-hidden />}
          onClick={signOutNow}
        >
          {t("password.success.signInAgain", "Sign in again")}
        </Button>
      </div>
    </div>
  );
}
