/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from 'react-i18next'
import { useNavigate } from '@tanstack/react-router'
import UnauthenticatedLayout from '@/components/layout/unauthenticated/unauthenticated-layout'
import { getAccessToken } from '@/lib/auth'

export default function Landing() {
  const { t } = useTranslation('common')
  const navigate = useNavigate()

  const handleDashboardClick = () => {
    const token = getAccessToken()
    if (token) {
      void navigate({ to: '/home' })
    } else {
      void navigate({ to: '/login' })
    }
  }

  return (
    <UnauthenticatedLayout>
      <div className="flex items-center justify-center min-h-[80vh]">
        <button
          onClick={handleDashboardClick}
          className="px-6 py-3 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors"
        >
          {t('dashboard', 'Dashboard')}
        </button>
      </div>
    </UnauthenticatedLayout>
  )
}
