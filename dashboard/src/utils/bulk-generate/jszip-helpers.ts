/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type JSZip from 'jszip'

/** Returns a zip subfolder or throws if creation failed. */
export function requireFolder(parent: JSZip, name: string): JSZip {
  const folder = parent.folder(name)
  if (!folder) {
    throw new Error(`Failed to create zip folder: ${name}`)
  }
  return folder
}
