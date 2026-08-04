/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { ErrorBoundary as ReactErrorBoundary, type FallbackProps } from 'react-error-boundary'
import { useNavigate } from '@tanstack/react-router'
import ErrorPage from '../../pages/error/error'

interface ErrorBoundaryProps {
  children: React.ReactNode
}

export default function ErrorBoundary({ children }: ErrorBoundaryProps) {
  const navigate = useNavigate()

  const handleError = (error: unknown, errorInfo: React.ErrorInfo) => {
    console.error('Error caught by boundary:', error, errorInfo)
    void navigate({ to: '/error' })
  }

  return (
    <ReactErrorBoundary
      FallbackComponent={ErrorFallback}
      onError={handleError}
      onReset={() => {
        // Reset error state and navigate home
        void navigate({ to: '/' })
      }}
    >
      {children}
    </ReactErrorBoundary>
  )
}

function ErrorFallback(_props: FallbackProps) {
  // Show error page component (it will handle its own navigation)
  return <ErrorPage />
}

