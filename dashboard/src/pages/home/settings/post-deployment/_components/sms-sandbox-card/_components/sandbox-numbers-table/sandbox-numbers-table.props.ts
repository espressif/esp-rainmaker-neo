/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { SMSSandboxPhoneNumber } from "@aws-sdk/client-sns";

export interface SandboxNumbersTableProps {
  numbers: SMSSandboxPhoneNumber[];
  isLoading: boolean;
  /** Re-sends the one-time password to one number. */
  onResend: (phoneNumber: string) => Promise<void>;
  /** Sends the SNS verify call for one number. */
  onVerify: (phoneNumber: string, oneTimePassword: string) => Promise<void>;
  /** Sends the SNS delete call for one number. */
  onDelete: (phoneNumber: string) => Promise<void>;
  /** A verification only flips a status, so the host can stay on the page it is showing. */
  onVerified: () => void;
  /** A removal shifts every page boundary, so the host should start over from the first page. */
  onDeleted: () => void;
}
