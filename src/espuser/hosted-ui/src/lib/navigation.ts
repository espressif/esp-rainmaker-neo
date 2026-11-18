/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** The parts of a click that decide where a link opens. */
export interface ClickKeys {
  button: number
  metaKey: boolean
  ctrlKey: boolean
  shiftKey: boolean
  altKey: boolean
}

/** opensHere is a plain primary click, which replaces this page; a modified or middle click opens a new tab and leaves this sign-in usable. */
export function opensHere(click: ClickKeys): boolean {
  return click.button === 0 && !click.metaKey && !click.ctrlKey && !click.shiftKey && !click.altKey
}
