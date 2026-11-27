/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { defineConfig } from 'vite'

export default defineConfig({
  // Relative paths, so index.html names its files ./assets/…, which the authorize Lambda inlines into every page it serves.
  base: './',
})
