/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react"

import type { CustomIconId } from "./custom-icon.config"

export interface CustomIconProps {
  type: CustomIconId
  size?: number
  fallback?: ComponentType<{ size?: number }>
  className?: string
}
