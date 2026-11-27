/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { render } from 'preact'
import { App } from './app.tsx'
import './styles.css'

render(<App />, document.getElementById('root')!)
