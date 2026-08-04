/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Node runtime adapter for NVS packing.
 *
 * Loads Pyodide via the `pyodide` npm package (WASM, no system Python) and
 * installs the pinned esp-idf-nvs-partition-gen package, then delegates packing
 * to the shared core. Used by the matter-gen CLI. Importing this module pulls
 * in the Node-only `pyodide` package, so browser code must not import it.
 */

// Pyodide instances are untyped Python proxies — see the note in `nvs-core.ts`.
/* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */

import {
  installNvsPackage,
  packRowsWithPyodide,
  MATTER_PARTITION_SIZE,
  type NvsRow,
} from './nvs-core'

let pyodidePromise: Promise<any> | null = null

/** Load (once) a Node Pyodide instance with esp-idf-nvs-partition-gen installed. */
export function loadNodePyodide(): Promise<any> {
  if (!pyodidePromise) {
    pyodidePromise = (async () => {
      const { loadPyodide } = await import('pyodide')
      const pyodide = await loadPyodide()
      await installNvsPackage(pyodide)
      return pyodide
    })()
  }
  return pyodidePromise
}

/** Pack NVS rows into a partition binary under Node. */
export async function generateNvsBinNode(
  rows: NvsRow[],
  partitionSize: number = MATTER_PARTITION_SIZE,
): Promise<Uint8Array> {
  const pyodide = await loadNodePyodide()
  return packRowsWithPyodide(pyodide, rows, partitionSize)
}
