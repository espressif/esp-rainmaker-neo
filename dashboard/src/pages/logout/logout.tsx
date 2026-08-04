/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { clearAuthTokens } from '@/lib/auth'

export default function Logout() {
  const { t } = useTranslation("common")
  useEffect(() => {
    clearAuthTokens()

    // Brief delay so the user sees the loading state
    setTimeout(() => {
      window.location.href = '/login'
    }, 500)
  }, [])

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="text-center">
        <div className="h-8 w-8 mx-auto mb-4 animate-spin rounded-full border-4 border-primary border-t-transparent" />
        <p className="text-sm text-muted-foreground">{t('signingOut', 'Signing out...')}</p>
      </div>
    </div>
  )
}
