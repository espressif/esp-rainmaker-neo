/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface OtaJobGroupFilterDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Draft group name being edited, or undefined when nothing is selected. */
  value?: string;
  onValueChange: (groupName: string | undefined) => void;
  /** Commit the draft to the applied filter and close. */
  onApply: () => void;
  /** Discard the draft and close. */
  onCancel: () => void;
}
