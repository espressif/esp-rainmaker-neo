/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { AppConfig } from './types.ts';

/**
 * Define application configuration with type safety
 * @param config - Application configuration object
 * @returns The same configuration object (enables type checking)
 *
 * Deliberately a leaf module rather than part of the `app-config` barrel: the
 * barrel is browser-facing, and `vite.config.ts` loads `app.config.ts` in Node
 * to build the document head (vite-plugins/app-head.ts). Keeping this separate
 * means a browser-only import added to the barrel can't break the Vite config.
 */
export function defineConfig(config: AppConfig): AppConfig {
  return config;
}
