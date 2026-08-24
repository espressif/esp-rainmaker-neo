/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ChangePasswordRequest, UserAuthFactors } from "@/api";

/**
 * Which form this admin needs. An admin seeded after passwordless sign-in has no
 * password to confirm, and Cognito accepts `ChangePassword` without a previous
 * password only for them.
 */
export type SetPasswordMode = "change" | "set";

export function passwordModeFor(
  factors: UserAuthFactors | undefined,
): SetPasswordMode {
  // Undefined means the lookup has not answered, or failed against a pool that
  // predates the feature. Assuming a password renders the pre-existing form, which
  // is correct for every admin who has one and merely inconvenient for those who
  // don't — the reverse would send a request Cognito rejects.
  return factors?.hasPassword === false ? "set" : "change";
}

interface PasswordFormValues {
  old_password: string;
  new_password: string;
  confirm_password: string;
}

export function changePasswordRequestFor(
  mode: SetPasswordMode,
  accessToken: string,
  values: PasswordFormValues,
): ChangePasswordRequest {
  // The key is omitted rather than blanked: Cognito's PreviousPassword pattern is
  // `[\S]+`, so an empty string is a validation error, not "no previous password".
  if (mode === "set") {
    return { access_token: accessToken, new_password: values.new_password };
  }
  return {
    access_token: accessToken,
    old_password: values.old_password,
    new_password: values.new_password,
  };
}
