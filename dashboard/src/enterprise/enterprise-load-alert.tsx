/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from 'react-i18next'
import { Alert } from '@espressif/dashboard-ui-components/components'
import { useEnterpriseStore } from './enterprise.store'

/**
 * Tells the admin that this deployment has enterprise features which failed to load as a whole. The `error` status only arises where the deployment names a plugins bucket, so a community dashboard never shows this.
 */
export default function EnterpriseLoadAlert() {
  const { t } = useTranslation('common')
  const status = useEnterpriseStore((s) => s.status)

  if (status !== 'error') {
    return null
  }
  return (
    <div className="px-4 pt-4">
      <Alert variant="soft" color="error">
        {t(
          'enterprise.loadFailed',
          "This deployment's enterprise features could not be loaded. Reload the page to try again.",
        )}
      </Alert>
    </div>
  )
}
