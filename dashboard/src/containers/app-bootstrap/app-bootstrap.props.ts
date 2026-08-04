/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from 'react'

export interface AppBootstrapProps {
  /** App tree rendered once the runtime config is available. */
  children: ReactNode
}
