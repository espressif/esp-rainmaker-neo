/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface EmailFormProps {
  /** Prefill from the flow store, so Back-and-forth keeps the typed address. */
  defaultUsername: string;
  isSubmitting: boolean;
  onSubmit: (username: string) => void;
}
