/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { defineConfig } from 'vite'

export default defineConfig({
  // Embedded in the authorize Lambda and served under its stage prefix, so every path is relative; the Lambda swaps the bundle's ./assets/ for the absolute prefix of the route it answers.
  base: './',
})
