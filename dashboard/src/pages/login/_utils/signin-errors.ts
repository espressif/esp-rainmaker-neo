/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type { LocalizedMessage } from "@/lib/auth";
import type { LoginSearch } from "../_schema/login-search.schema";

/**
 * The single error message a step page shows, in priority order: a rejected
 * password submit, then a failure from the identify/choose/otp flow (or the
 * store's persisted notice), then a generic fallback for anything else the raw
 * mutation raised. `null` means nothing failed, so no alert should render.
 */
export function resolveErrorMessage(
  t: TFunction,
  loginError: string | null,
  flowError: LocalizedMessage | null,
  rawError: Error | null,
): string | null {
  if (!loginError && !flowError && !rawError) {
    return null;
  }
  if (loginError) {
    return loginError;
  }
  if (flowError) {
    return t(flowError.key, flowError.fallback);
  }
  return (
    rawError?.message ||
    t(
      "common:errorMessage",
      "An unexpected error occurred. Please try again later.",
    )
  );
}

/**
 * The success banner for the two ways account settings can send an admin back here.
 * "success" is a password change; "set" is a first password adopted by an admin who
 * previously had none — reusing the "reset" wording for that case would be false, so
 * each outcome gets its own title and message.
 */
export function passwordOutcomeCopy(
  t: TFunction,
  outcome: LoginSearch["reset"],
): { title: string; message: string } | null {
  if (outcome === "set") {
    return {
      title: t("setSuccessTitle", "Password set"),
      message: t(
        "setSuccessMessage",
        "Your password has been set. Sign in with it, or with a code, from now on.",
      ),
    };
  }
  if (outcome === "success") {
    return {
      title: t("resetSuccessTitle", "Password updated"),
      message: t(
        "resetSuccessMessage",
        "Your password has been updated. Sign in with your new password.",
      ),
    };
  }
  return null;
}
