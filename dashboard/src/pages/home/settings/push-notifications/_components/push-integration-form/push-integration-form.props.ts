/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface PushIntegrationFormProps {
  /**
   * Invoked when the user cancels. The host (sheet / dialog / page) decides what
   * closing means, keeping this form decoupled from its container.
   */
  onCancel?: () => void;
  /**
   * Invoked after a successful registration. The host maps this to its own
   * close lifecycle; the form never touches its container directly.
   */
  onSuccess?: () => void;
}
