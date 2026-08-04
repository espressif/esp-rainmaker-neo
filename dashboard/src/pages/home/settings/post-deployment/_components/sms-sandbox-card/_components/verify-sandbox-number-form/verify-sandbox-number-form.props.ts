/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface VerifySandboxNumberFormProps {
  /** Sends the SNS verify call. Rejections are mapped to copy by the form, not by the caller. */
  onSubmit: (oneTimePassword: string) => Promise<void>;
  /** Called once the number is verified, so the host can refresh its list. */
  onSuccess: () => void;
  onCancel: () => void;
}
