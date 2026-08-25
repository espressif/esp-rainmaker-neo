/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { z } from 'zod'

/**
 * Deploy-time capability cards contributed by optional backend modules.
 *
 * A module's CDK stack carries one `DashboardCapability<Id>` output per card
 * (see `ModuleContext.add_dashboard_capability` in `cdk/apps/rmng.py`); the
 * outputs pipeline publishes them into the client-outputs document, and this
 * collector turns them into `EXTRA_CAPABILITIES`. The card exists exactly
 * while the contributing stack is deployed — community deployments have no
 * such outputs and see nothing. The card is UX only: enforcement lives in the
 * module's backend register hook, which receives the capability id.
 */

export const extraCapabilitySchema = z.object({
  /** Capability string sent on node registration; also the card id. */
  id: z.string().regex(/^[a-z][a-z0-9-]{0,31}$/),
  labelFallback: z.string().min(1),
  /** i18n key overrides; when absent the conventional keys below are derived. */
  labelKey: z.string().optional(),
  descriptionKey: z.string().optional(),
  descriptionFallback: z.string().optional(),
  /** Icon name in the enterprise icon registry; unknown names fall back. */
  icon: z.string(),
})

export type ExtraCapability = z.infer<typeof extraCapabilitySchema>

/**
 * Conventional i18n keys for a capability's label/description, inside the
 * contributing module's plugin namespace (`enterprise-<id>`, same convention as
 * `enterpriseNamespace()`); a plugin ships translations under these keys unless
 * the capability entry overrides them explicitly.
 */
export function extraCapabilityLabelKey(capability: ExtraCapability): string {
  return capability.labelKey ?? `enterprise-${capability.id}:capabilities.${capability.id}.label`
}

export function extraCapabilityDescriptionKey(capability: ExtraCapability): string {
  return (
    capability.descriptionKey ??
    `enterprise-${capability.id}:capabilities.${capability.id}.description`
  )
}

const OUTPUT_KEY_PREFIX = 'DashboardCapability'

/**
 * Collect every `DashboardCapability*` output across the stack-keyed
 * client-outputs document. Malformed or schema-invalid entries are skipped
 * loudly (console error), never thrown — a bad capability must not take the
 * config load down with it. Duplicate ids across stacks keep the first seen.
 */
export function collectExtraCapabilities(
  outputs: Record<string, Record<string, unknown> | undefined>,
): ExtraCapability[] {
  const seen = new Set<string>()
  const collected: ExtraCapability[] = []
  for (const [stackName, stackOutputs] of Object.entries(outputs)) {
    if (!stackOutputs) {
      continue
    }
    for (const [key, value] of Object.entries(stackOutputs)) {
      // The non-string check also skips the Alexa stacks' nested `regions` objects.
      if (!key.startsWith(OUTPUT_KEY_PREFIX) || typeof value !== 'string') {
        continue
      }
      let parsed: unknown
      try {
        parsed = JSON.parse(value)
      } catch {
        console.error(`[config] malformed dashboard capability ${stackName}.${key}`)
        continue
      }
      const result = extraCapabilitySchema.safeParse(parsed)
      if (!result.success) {
        console.error(`[config] invalid dashboard capability ${stackName}.${key}`, result.error)
        continue
      }
      if (!seen.has(result.data.id)) {
        seen.add(result.data.id)
        collected.push(result.data)
      }
    }
  }
  return collected
}
