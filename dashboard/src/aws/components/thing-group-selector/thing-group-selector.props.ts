/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react"

export interface ThingGroupSelectorProps {
  value?: string
  onSelect: (groupName: string | undefined) => void
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
  /** Only offer parent-less (top-level) groups — used by the register flow. */
  topLevelOnly?: boolean
  /**
   * Enable an optional subgroup picker: a checkbox that, once a group is
   * selected, reveals a second selector scoped to that group's children.
   */
  allowSubgroupSelection?: boolean
  subgroupValue?: string
  onSubgroupSelect?: (subgroupName: string | undefined) => void
  subgroupLabel?: ReactNode
}
