/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TargetSelection } from "@aws-sdk/client-iot";

export interface OtaJobTargetSelectionFilterProps {
  value: TargetSelection | null;
  onChange: (targetSelection: TargetSelection | null) => void;
}
