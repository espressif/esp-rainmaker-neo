/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from 'react-i18next'
import { useLocation } from '@tanstack/react-router'
import { RefreshCw } from 'lucide-react'
import { PageLoader } from '@espressif/dashboard-ui-components/common'
import { Button, FullSizeError } from '@espressif/dashboard-ui-components/components'
import { useRuntimeConfigQuery } from '@/api/config'
import { BOOTSTRAP_EXEMPT_PATHS } from '@/config/app-routes.config'
import { useConfigStore } from '@/stores/config.store'
import type { AppBootstrapProps } from './app-bootstrap.props'

/**
 * Gates the app behind runtime-config loading.
 *
 * Cached-first: once a config exists in the persisted store, children render
 * immediately and the query refreshes it silently in the background — a failed
 * refresh never blocks a returning user. Only the first-ever load (no cache)
 * shows the full-screen loader and, on hard failure, a retryable error.
 *
 * Routes flagged `skipBootstrap` bypass the gate entirely: they render from bundled
 * assets alone, so an unreachable backend must not hide them.
 */
export default function AppBootstrap({ children }: AppBootstrapProps) {
  const { t } = useTranslation("common")
  const { pathname } = useLocation()
  const cachedConfig = useConfigStore((state) => state.config)

  const isExempt = BOOTSTRAP_EXEMPT_PATHS.some((exemptPath) =>
    pathname.startsWith(exemptPath)
  )
  const { isError, isFetching, refetch } = useRuntimeConfigQuery({
    enabled: !isExempt,
  })

  if (isExempt) {
    return <>{children}</>
  }

  if (cachedConfig) {
    return <>{children}</>
  }

  if (isError) {
    return (
      <FullSizeError title={t('bootstrap.errorTitle', 'Unable to start the dashboard')}>
        <div className="flex flex-col items-center gap-4">
          <p className="max-w-md text-center text-sm text-muted-foreground">
            {t(
              'bootstrap.errorMessage',
              'We could not load the app configuration. Check your connection and try again.'
            )}
          </p>
          <Button
            onClick={() => void refetch()}
            loading={isFetching}
            startIcon={<RefreshCw className="h-4 w-4" />}
          >
            {t('common:actions.retry', 'Retry')}
          </Button>
        </div>
      </FullSizeError>
    )
  }

  return <PageLoader />
}
