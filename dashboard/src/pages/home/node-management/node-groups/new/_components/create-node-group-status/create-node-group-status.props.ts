/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type {
  CreateNodeGroupResult,
  CreateNodeGroupStatus,
} from "../../_hooks/use-create-node-group-orchestration";

export interface CreateNodeGroupStatusProps {
  status: CreateNodeGroupStatus;
  /** Present in the success state; identifies the created group for navigation. */
  result?: CreateNodeGroupResult | null;
  /** Present in the failure state; specific reason when detectable. */
  errorMessage?: string;
  onBackToGroups: () => void;
  onViewGroupDetails: () => void;
  /** Closes the dialog and returns to the form (values preserved) to fix and resubmit. */
  onEditAndRetry: () => void;
}
