/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface OtaJobGroupFilterBodyProps {
  /** Draft group name being edited, or undefined when nothing is selected. */
  value?: string;
  onValueChange: (groupName: string | undefined) => void;
  /** Commit the draft to the applied filter. */
  onApply: () => void;
  /** Discard the draft. */
  onCancel: () => void;
}
