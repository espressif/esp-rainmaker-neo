/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { GenerateStatus as GenerateStatusValue } from "../../_hooks/use-generate-orchestration";

export interface GenerateStatusProps {
  status: GenerateStatusValue;
  errorMessage?: string;
  /** Whether the package has been downloaded — gates the register action. */
  downloaded: boolean;
  onDownload: () => void;
  onRegisterNodes: () => void;
  onRetry: () => void;
}
