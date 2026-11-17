/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Every extension slot the dashboard offers. Names describe a place on a page, never the feature that fills it. A `*` stands for one generated segment: the register form derives one inline slot per section, so `nodes.register.*.extra` covers `nodes.register.tags.extra`.
 *
 * Adding, renaming or removing an entry changes the contract with deployed plugins; the plugin SDK's own catalog is checked against this list when plugins are built.
 */
export const EXTENSION_SLOT_IDS = [
  'account-settings.preferences.extra',
  'nodes.register.*.extra',
  'nodes.register.sections',
] as const

type SlotIdOf<Pattern extends string> = Pattern extends `${infer Head}*${infer Tail}`
  ? `${Head}${string}${Tail}`
  : Pattern

export type ExtensionSlotId = SlotIdOf<(typeof EXTENSION_SLOT_IDS)[number]>

/** Whether this dashboard offers the slot: ('nodes.register.tags.extra') -> true via `nodes.register.*.extra`; a `*` never spans a dot. */
export function isKnownExtensionSlot(slotId: string): boolean {
  return EXTENSION_SLOT_IDS.some((pattern) => {
    const source = pattern
      .split('*')
      .map((part) => part.replace(/[.+?^${}()|[\]\\]/g, '\\$&'))
      .join('[^.]+')
    return new RegExp(`^${source}$`).test(slotId)
  })
}
