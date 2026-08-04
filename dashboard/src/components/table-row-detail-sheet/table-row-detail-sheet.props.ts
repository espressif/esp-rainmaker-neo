/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";

export interface TableRowDetailSheetProps {
  label?: string | ReactNode;
  contentClassName?: string;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
}
