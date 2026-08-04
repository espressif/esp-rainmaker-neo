/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface CreateNodeGroupFormProps {
  /** Called when the user abandons the form (mapped by the container). */
  onCancel?: () => void;
  /** Called with the new group name once creation succeeds (wired later). */
  onSuccess?: (groupName: string) => void;
}
