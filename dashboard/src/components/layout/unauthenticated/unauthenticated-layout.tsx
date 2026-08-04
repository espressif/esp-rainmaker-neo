/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from 'react'
import PublicHeader from './public-header/public-header'

interface UnauthenticatedLayoutProps {
  children: ReactNode
}

export default function UnauthenticatedLayout({ children }: UnauthenticatedLayoutProps) {
  return (
    <div className="min-h-screen flex flex-col">
      <PublicHeader />
      <main 
        className="flex-1"
        style={{ paddingTop: 'var(--espd-header-height)' }}
      >
        {children}
      </main>
    </div>
  )
}

