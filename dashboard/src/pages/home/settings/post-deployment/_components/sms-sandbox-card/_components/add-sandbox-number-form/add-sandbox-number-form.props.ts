/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface AddSandboxNumberFormProps {
  /** Sends the SNS create call. Rejections are mapped to copy by the form, not by the caller. */
  onSubmit: (phoneNumber: string) => Promise<void>;
  /** Called once the number is registered, so the host can refresh its list. */
  onSuccess: () => void;
  disabled?: boolean;
}
