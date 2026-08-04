/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface SandboxNumberRowActionsProps {
  phoneNumber: string;
  /** Raw upstream verification status; drives which actions are offered. */
  status?: string;
  isResending: boolean;
  isDeleting: boolean;
  /** Whether this row's inline one-time-code form is open. */
  isVerifying: boolean;
  onResend: () => void;
  onStartVerify: () => void;
  onCancelVerify: () => void;
  onVerifySubmit: (oneTimePassword: string) => Promise<void>;
  onVerified: () => void;
  onDelete: () => Promise<void>;
  onCancelDelete: () => void;
}
