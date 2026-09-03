/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { defineConfig } from 'vitest/config'

// Isolated unit-test config for the standalone matter-gen module.
// Kept separate from vite.config.ts so app plugins (router, svgr, …) don't
// run during unit tests — these are pure crypto/encoding units.
export default defineConfig({
  // Don't load the app's postcss.config.ts (needs ts-node); tests use no CSS.
  css: { postcss: { plugins: [] } },
  test: {
    environment: 'node',
    // Page-level `_utils` are included too, but this config resolves no `@/`
    // alias and provides no DOM — so anything matched here must be a pure unit
    // whose non-relative imports are type-only.
    include: [
      'src/utils/**/*.test.ts',
      'src/lib/**/*.test.ts',
      'src/pages/**/*.test.ts',
      'src/stores/**/*.test.ts',
    ],
  },
})
