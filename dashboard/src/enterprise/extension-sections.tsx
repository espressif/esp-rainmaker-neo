/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from 'react'
import type { TFunction } from 'i18next'
import { ErrorBoundary } from 'react-error-boundary'
import { findExtensionContributions, type EnterpriseFeature } from './enterprise.store'
import { enterpriseNamespace, type EnterpriseComponentProps } from './enterprise.types'
import { resolveEnterpriseIcon } from './enterprise-icons'

/**
 * Section-shaped extension slots: a contribution becomes a whole section of a
 * host form (own tab, icon, label, content card), so a module can add e.g. a
 * full multi-form section without any open-source change. The contribution's
 * presentation fields (`labelKey`/`labelFallback`/`icon`) drive the chrome;
 * the host renders it with the same components as its built-in sections.
 */
export interface EnterpriseSection {
  id: string
  Icon: ComponentType<{ className?: string; 'aria-hidden'?: boolean }>
  label: string
  Content: ComponentType
}

/**
 * Build sections from loaded features' contributions targeting a section-shaped
 * slot. `context` is handed to each plugin component (per-slot contract in the
 * SDK); each component renders inside its own error boundary. Entries without a
 * label, or naming an unregistered component, are skipped loudly in DEV.
 */
export function collectEnterpriseSections(
  features: Record<string, EnterpriseFeature>,
  slotId: string,
  t: TFunction,
  context?: EnterpriseComponentProps['context'],
): EnterpriseSection[] {
  const sections: EnterpriseSection[] = []
  for (const { feature, extension } of findExtensionContributions(features, slotId)) {
    const Component = feature.components[extension.componentId]
    if (!Component || (!extension.labelKey && !extension.labelFallback)) {
      if (import.meta.env.DEV) {
        console.warn(
          `[enterprise] section contribution "${feature.entry.id}:${extension.componentId}" for slot "${slotId}" is missing its ${Component ? 'label' : 'component'} — skipped`,
        )
      }
      continue
    }
    const ns = enterpriseNamespace(feature.entry.id)
    const label = extension.labelKey
      ? t(`${ns}:${extension.labelKey}`, extension.labelFallback ?? extension.labelKey)
      : (extension.labelFallback ?? '')
    sections.push({
      id: `ent-${feature.entry.id}-${extension.componentId}`,
      Icon: resolveEnterpriseIcon(extension.icon ?? ''),
      label,
      Content: function EnterpriseSectionContent() {
        return (
          <ErrorBoundary
            fallbackRender={() => null}
            onError={(error) =>
              console.error(
                `[enterprise] section "${feature.entry.id}:${extension.componentId}" in slot "${slotId}" crashed:`,
                error,
              )
            }
          >
            <Component context={context} />
          </ErrorBoundary>
        )
      },
    })
  }
  return sections
}
