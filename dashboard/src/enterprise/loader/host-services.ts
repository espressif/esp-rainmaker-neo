/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { sigv4Request } from '@/api/sigv4-client'
import { getApiGatewayUrl } from '@/lib/config'

export interface HostRequestOptions {
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  url: string
  body?: unknown
}

/**
 * The dashboard's own services a plugin may call, exposed to plugins as `host` by the plugin SDK (its `plugin-sdk/src/host.ts` mirrors this shape). These are adapters with their own types, not the internal functions themselves, so refactoring `sigv4Request` cannot silently change what deployed plugins receive. Kept deliberately small: every member is a contract.
 */
export const hostServices = {
  /** Signed request to this deployment's API with the signed-in admin's credentials; rejects on a non-2xx response. */
  sigv4Request<T>(options: HostRequestOptions): Promise<T> {
    return sigv4Request<T>({ method: options.method, url: options.url, body: options.body })
  },
  /** Base URL of this deployment's API. */
  getApiGatewayUrl(): string {
    return getApiGatewayUrl()
  },
}
