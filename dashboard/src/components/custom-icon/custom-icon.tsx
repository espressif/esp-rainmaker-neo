/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { cn } from "@/utils/utils"
import { customIconRegistry } from "./custom-icon.config"
import type { CustomIconProps } from "./custom-icon.props"

function CustomIcon({ type, size = 18, fallback: Fallback, className }: CustomIconProps) {
  const entry = customIconRegistry[type]
  const Icon = entry?.component ?? Fallback
  if (!Icon) {return null}

  return (
    <span className={cn("inline-flex shrink-0", className)} aria-hidden>
      <Icon size={size} />
    </span>
  )
}

export { CustomIcon }
