/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from 'react'
import { ErrorBoundary } from 'react-error-boundary'
import { findExtensionContributions, useEnterpriseStore } from './enterprise.store'
import type { EnterpriseComponentProps } from './enterprise.types'
import type { ExtensionSlotId } from './slots'

/**
 * Named mount point for enterprise plugin components inside an open-source screen.
 *
 * Loaded features contribute via `contributes.extensions` in the plugin index;
 * this slot renders every contribution targeting its id, sorted, each inside its own
 * error boundary so one broken plugin cannot take the screen down. With no
 * contributions (community mode, still loading) it renders nothing —
 * zero visual footprint. Slot ids are listed in `slots.ts`; ids and their `context` shapes
 * are a versioned public contract, published in the plugin SDK.
 */
export function ExtensionSlot({ id, context }: { id: ExtensionSlotId } & EnterpriseComponentProps) {
  const features = useEnterpriseStore((s) => s.features)
  const matches = useMemo(() => findExtensionContributions(features, id), [features, id])

  if (matches.length === 0) {
    return null
  }
  return (
    <>
      {matches.map(({ feature, extension }) => {
        const Component = feature.components[extension.componentId]
        if (!Component) {
          if (import.meta.env.DEV) {
            console.warn(
              `[enterprise] extension from "${feature.entry.id}" targets slot "${id}" with unregistered component "${extension.componentId}" — skipped`,
            )
          }
          return null
        }
        return (
          <ErrorBoundary
            key={`${feature.entry.id}:${extension.componentId}`}
            fallbackRender={() => null}
            onError={(error) =>
              console.error(
                `[enterprise] extension "${feature.entry.id}:${extension.componentId}" in slot "${id}" crashed:`,
                error,
              )
            }
          >
            <Component context={context} />
          </ErrorBoundary>
        )
      })}
    </>
  )
}
