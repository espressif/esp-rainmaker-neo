/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";

export interface ResourceHeadingDescriptionProps {
  /** Status badge (or any short indicator) shown on the first line. */
  badge?: ReactNode;
  /** Resource description shown below the badge. Omitted when absent. */
  description?: ReactNode;
}
