/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from 'i18next'
import type { BreadcrumbItemConfig } from '@espressif/dashboard-ui-components/layouts'
import { ENTERPRISE_ROUTE_PREFIX } from './enterprise.types'
import { findEnterpriseRoute } from './enterprise.store'
import type { EnterpriseFeature } from './enterprise.store'

/**
 * Append the crumb a matched enterprise route declares in its manifest. Static breadcrumbs come
 * only from `routesConfig`, and the `/home/ent/$` entry deliberately has none — so for any
 * enterprise path the base chain is exactly [Home], and Home is the only crumb to re-link.
 */
export function withEnterpriseBreadcrumb(
  crumbs: BreadcrumbItemConfig[],
  features: Record<string, EnterpriseFeature>,
  pathname: string,
  t: TFunction,
): BreadcrumbItemConfig[] {
  if (!pathname.startsWith(ENTERPRISE_ROUTE_PREFIX)) {
    return crumbs
  }
  const breadcrumb = findEnterpriseRoute(features, pathname)?.route.breadcrumb
  if (!breadcrumb) {
    return crumbs
  }
  const label = breadcrumb.i18nKey
    ? t(breadcrumb.i18nKey, breadcrumb.fallback)
    : breadcrumb.fallback
  const relinked = crumbs.map((crumb) =>
    crumb.path ? crumb : { ...crumb, path: '/home' },
  )
  return [...relinked, { label }]
}
