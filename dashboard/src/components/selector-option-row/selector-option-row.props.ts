/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react"

export interface SelectorOptionRowProps {
  /** Leading avatar/icon slot (e.g. an IconAvatar or a domain avatar). */
  avatar: ReactNode
  /** Primary line — the resource name. */
  label: string
  /** Optional muted second line (id, size, etc.). */
  secondaryText?: string
}
