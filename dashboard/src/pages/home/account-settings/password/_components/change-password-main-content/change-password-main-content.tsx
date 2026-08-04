/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { ChangePasswordForm } from "../change-password-form";
import { ChangePasswordSuccess } from "../change-password-success";
import type { ChangePasswordMainContentProps } from "./change-password-main-content.props";

/**
 * Picks the card body: the confirmation once the password has changed, the form
 * otherwise. There is no loading or empty branch — the form needs no data to render,
 * and its own failure state lives inside the form.
 */
export default function ChangePasswordMainContent({
  isPasswordChanged,
  onPasswordChanged,
}: ChangePasswordMainContentProps) {
  if (isPasswordChanged) {
    return <ChangePasswordSuccess />;
  }

  return <ChangePasswordForm onSuccess={onPasswordChanged} />;
}
