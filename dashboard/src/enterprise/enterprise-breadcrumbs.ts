/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from 'i18next'
import type { BreadcrumbItemConfig } from '@espressif/dashboard-ui-components/layouts'
import { ENTERPRISE_ROUTE_PREFIX } from './enterprise.types'
import { findEnterpriseRoute } from './enterprise.store'
import type { EnterpriseFeature, EnterpriseStatus } from './enterprise.store'
import { findLockedCatalogEntry } from './catalog'

/** The crumb for an enterprise path: the locked catalog entry's name, else what the matched plugin route declares. */
function enterpriseCrumbLabel(
  features: Record<string, EnterpriseFeature>,
  status: EnterpriseStatus,
  pathname: string,
  t: TFunction,
): string | undefined {
  const locked = findLockedCatalogEntry(status, features, pathname)
  if (locked) {
    return t(locked.labelKey, locked.labelFallback)
  }
  const breadcrumb = findEnterpriseRoute(features, pathname)?.route.breadcrumb
  if (!breadcrumb) {
    return undefined
  }
  return breadcrumb.i18nKey ? t(breadcrumb.i18nKey, breadcrumb.fallback) : breadcrumb.fallback
}

/**
 * Append the crumb a matched enterprise route declares in its manifest. Static breadcrumbs come
 * only from `routesConfig`, and the `/home/ent/$` entry deliberately has none — so for any
 * enterprise path the base chain is exactly [Home], and Home is the only crumb to re-link.
 */
export function withEnterpriseBreadcrumb(
  crumbs: BreadcrumbItemConfig[],
  features: Record<string, EnterpriseFeature>,
  status: EnterpriseStatus,
  pathname: string,
  t: TFunction,
): BreadcrumbItemConfig[] {
  if (!pathname.startsWith(ENTERPRISE_ROUTE_PREFIX)) {
    return crumbs
  }
  const label = enterpriseCrumbLabel(features, status, pathname, t)
  if (!label) {
    return crumbs
  }
  const relinked = crumbs.map((crumb) =>
    crumb.path ? crumb : { ...crumb, path: '/home' },
  )
  return [...relinked, { label }]
}
