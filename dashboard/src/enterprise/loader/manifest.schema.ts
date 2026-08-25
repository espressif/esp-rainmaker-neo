/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { z } from 'zod'

/**
 * Schema of a per-feature signed manifest, one per feature directory in the central
 * enterprise assets bucket (spec: dashboard_modules/docs/enterprise-features-runtime-loading.md
 * in the enterprise superproject).
 * Parsed only after the manifest's signature has been verified. There is no user/entitlement
 * map: entitlement is the bucket policy — a deployment either can read the bucket or cannot.
 */

const sidebarItemSchema = z.object({
  id: z.string(),
  labelKey: z.string(),
  labelFallback: z.string(),
  icon: z.string(),
  path: z.string(),
})

const sidebarContributionSchema = z.object({
  group: z.object({
    id: z.string(),
    merge: z.boolean().optional(),
    labelKey: z.string().optional(),
    labelFallback: z.string().optional(),
    icon: z.string().optional(),
  }),
  items: z.array(sidebarItemSchema),
})

const routeContributionSchema = z.object({
  path: z.string(),
  componentId: z.string(),
  breadcrumb: z
    .object({ i18nKey: z.string().optional(), fallback: z.string() })
    .optional(),
})

const extensionContributionSchema = z.object({
  /** Host extension-slot id the component renders into (the slot catalog lives in the plugin SDK). */
  slotId: z.string(),
  /** Key into the feature's registered components map. */
  componentId: z.string(),
  /** Render order within the slot; ties break on feature id, then declaration order. */
  order: z.number().optional(),
  // Presentation fields, consumed only by section-shaped slots (ignored by inline slots).
  labelKey: z.string().optional(),
  labelFallback: z.string().optional(),
  icon: z.string().optional(),
})

export const featureManifestSchema = z.object({
  schemaVersion: z.literal(1),
  issuedAt: z.string().optional(),
  id: z.string(),
  version: z.string(),
  /** Host-SDK semver range this bundle was built against. */
  sdk: z.string(),
  /** Bundle key *relative to the feature's own directory* — a manifest can only reference its own files. */
  bundle: z.string(),
  /** `sha384-<base64>` of the bundle bytes; checked before the code runs. */
  integrity: z.string(),
  /** Registry keys the bundle was externalized against; preflighted before import. */
  sharedModules: z.array(z.string()).optional(),
  contributes: z.object({
    sidebar: z.array(sidebarContributionSchema).optional(),
    routes: z.array(routeContributionSchema).optional(),
    extensions: z.array(extensionContributionSchema).optional(),
  }),
  /** Client-outputs stack keys the feature's backend needs (same idea as the deployed-stacks gate). */
  requiresBackend: z.array(z.string()).optional(),
})

export type ManifestFeature = z.infer<typeof featureManifestSchema>
export type SidebarContribution = z.infer<typeof sidebarContributionSchema>
export type RouteContribution = z.infer<typeof routeContributionSchema>
export type ExtensionContribution = z.infer<typeof extensionContributionSchema>
