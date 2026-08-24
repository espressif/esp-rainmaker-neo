/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { SmartThingsConfigGetResponse } from "@/api/integrations";

export interface SmartThingsConfigFormProps {
  /** Saved configuration used to seed the form; absent when configuring anew. */
  initialData?: SmartThingsConfigGetResponse;
  /**
   * Invoked when the user cancels. The host (sheet / dialog / page) decides what
   * closing means, keeping this form decoupled from its container.
   */
  onCancel?: () => void;
  /**
   * Invoked after a successful save or delete. The host maps this to its own
   * close lifecycle; the form never touches its container directly.
   */
  onSuccess?: () => void;
}
