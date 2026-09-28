/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/// <reference types="vite/client" />

declare module '*.module.scss' {
  const classes: { readonly [key: string]: string }
  export default classes
}

declare module '*.svg?react' {
  import type React from 'react'
  const ReactComponent: React.FC<React.SVGProps<SVGSVGElement>>
  export default ReactComponent
}

/** `customAuth.onboardingShowcase` from app.config.ts, inlined by `define` in vite.config.ts. */
declare const __ONBOARDING_SHOWCASE__: boolean

/**
 * Vite environment variables.
 *
 * Runtime settings come from the published client-outputs document at startup
 * (see `@/api/config`), so the only build-time variable is the URL of that
 * document. In dev it is derived into `.env.development.local` by
 * `scripts/derive-server-url.mjs`; in prod it is read from `/config.json`.
 */
interface ImportMetaEnv {
  readonly DEV: boolean
  readonly PROD: boolean
  readonly MODE: string
  readonly VITE_SERVER_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
