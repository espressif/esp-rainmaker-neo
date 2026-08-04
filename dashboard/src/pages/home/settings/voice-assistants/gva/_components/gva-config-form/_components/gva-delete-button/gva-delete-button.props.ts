/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface GvaDeleteButtonProps {
  /** Runs the delete; may throw so the dialog stays open on failure. */
  onConfirm: () => void | Promise<void>;
  /** Delete request in flight — shows a spinner on the confirm button. */
  isDeleting: boolean;
  /** Disables the trigger (e.g. while the form is submitting). */
  disabled?: boolean;
  /** Inline error shown inside the dialog when a delete fails. */
  error?: string | null;
}
