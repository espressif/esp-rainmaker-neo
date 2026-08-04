/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Browser runtime adapter for NVS packing.
 *
 * Loads Pyodide from the CDN (as the dashboard has always done) and installs
 * the pinned esp-idf-nvs-partition-gen package, then delegates packing to the
 * shared core. Used by the dashboard UI (RainMaker bulk + Matter flows).
 */

// Pyodide instances are untyped Python proxies — see the note in `nvs-core.ts`.
/* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */

import {
  installNvsPackage,
  packRowsWithPyodide,
  RMAKER_PARTITION_SIZE,
  type NvsRow,
} from './nvs-core'

const PYODIDE_CDN = 'https://cdn.jsdelivr.net/pyodide/v0.26.4/full/'

let pyodidePromise: Promise<any> | null = null

declare global {
  interface Window {
    loadPyodide?: (options: { indexURL: string }) => Promise<any>
  }
}

/** Start loading Pyodide in the background. Safe to call multiple times. */
export function preloadPyodide(): Promise<any> {
  if (!pyodidePromise) {
    pyodidePromise = loadPyodideRuntime()
  }
  return pyodidePromise
}

async function loadPyodideRuntime(): Promise<any> {
  if (!window.loadPyodide) {
    await new Promise<void>((resolve, reject) => {
      const script = document.createElement('script')
      script.src = `${PYODIDE_CDN}pyodide.js`
      script.onload = () => resolve()
      script.onerror = () => reject(new Error('Failed to load Pyodide from CDN'))
      document.head.appendChild(script)
    })
  }

  const loadPyodide = window.loadPyodide
  if (!loadPyodide) {
    throw new Error('Pyodide script loaded but loadPyodide is unavailable')
  }
  const pyodide = await loadPyodide({ indexURL: PYODIDE_CDN })
  await installNvsPackage(pyodide)
  return pyodide
}

/** Pack NVS rows into a partition binary in the browser. */
export async function generateNvsBin(
  rows: NvsRow[],
  partitionSize: number = RMAKER_PARTITION_SIZE,
): Promise<Uint8Array> {
  const pyodide = await preloadPyodide()
  return packRowsWithPyodide(pyodide, rows, partitionSize)
}
