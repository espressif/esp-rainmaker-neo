/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import svgr from 'vite-plugin-svgr'
import path from 'path'
import { appHead } from './vite-plugins/app-head.ts'
import appConfig from './app.config.ts'

export default defineConfig({
  plugins: [
    appHead(),
    react(),
    svgr({
      include: '**/*.svg?react',
      svgrOptions: {
        icon: true,
      },
    }),
  ],
  define: {
    // Read at build time so a build with the showcase off leaves its chunk and clips out of dist/.
    __ONBOARDING_SHOWCASE__: JSON.stringify(appConfig.customAuth?.onboardingShowcase === true),
  },
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  server: {
    port: 5174,
  },
  build: {
    outDir: 'dist',
  },
})

