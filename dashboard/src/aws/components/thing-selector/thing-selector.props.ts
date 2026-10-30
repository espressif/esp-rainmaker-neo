/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react"

export interface ThingSelectorProps {
  value?: string
  onSelect: (thingName: string | undefined) => void
  onError?: (error: Error) => void
  label?: ReactNode
  placeholder?: string
  searchPlaceholder?: string
  emptyText?: string
  required?: boolean
  disabled?: boolean
  /** Clicking the selected node again clears it. Defaults to false. */
  clearable?: boolean
  /** Compact trigger for filter toolbars. Defaults to "default". */
  size?: "sm" | "default"
}
