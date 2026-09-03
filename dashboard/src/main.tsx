/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { createRoot } from 'react-dom/client'
import { RouterProvider } from '@tanstack/react-router'
import { createRouter } from '../app/router'
import { migrateLegacyAuthStorage } from './lib/auth'
import './styles/globals.css'

migrateLegacyAuthStorage()

const router = createRouter()

const rootEl = document.getElementById('root')
if (!rootEl) {
  throw new Error('Root element #root not found')
}

// Runtime config loading is handled inside React by AppBootstrap (see __root),
// so the router mounts immediately and the config gate drives loading/error UI.
createRoot(rootEl).render(<RouterProvider router={router} />)
