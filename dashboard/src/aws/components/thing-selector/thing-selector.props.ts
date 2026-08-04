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
  /** Allow clearing the selection. Defaults to true. */
  clearable?: boolean
  /** Compact trigger for filter toolbars. Defaults to "default". */
  size?: "sm" | "default"
}
