/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { SignatureV4 } from '@smithy/signature-v4'
import { Sha256 } from '@aws-crypto/sha256-browser'
import { HttpRequest } from '@smithy/protocol-http'
import { useAuthStore } from '@/stores/auth.store'
import { getAwsRegion, getApiGatewayUrl } from '@/lib/config'

export function getCredentials() {
  const { credentials, isCredentialsValid } = useAuthStore.getState()
  if (!credentials) {throw new Error('No AWS credentials available')}
  if (!isCredentialsValid()) {
    useAuthStore.getState().clearCredentials()
    throw new Error('AWS credentials expired')
  }
  return credentials
}

export async function signedFetch(method: string, path: string, body?: string): Promise<Response> {
  const creds = getCredentials()
  const region = getAwsRegion()
  const baseUrl = getApiGatewayUrl().replace(/\/$/, '')
  const fullUrl = baseUrl + path
  const url = new URL(fullUrl)

  const headers: Record<string, string> = {
    host: url.hostname,
  }
  if (body) {
    headers['content-type'] = 'application/json'
  }

  // Parse query params for SigV4 signing (must be included in canonical request)
  const query: Record<string, string> = {}
  url.searchParams.forEach((value, key) => {
    query[key] = value
  })

  const request = new HttpRequest({
    method,
    protocol: url.protocol,
    hostname: url.hostname,
    path: url.pathname,
    query,
    headers,
    body,
  })

  const signer = new SignatureV4({
    service: 'execute-api',
    region,
    credentials: {
      accessKeyId: creds.accessKeyId,
      secretAccessKey: creds.secretAccessKey,
      sessionToken: creds.sessionToken,
    },
    sha256: Sha256,
  })

  const signed = await signer.sign(request)

  // Build fetch headers — exclude 'host' (browser sets it automatically and forbids overriding)
  const fetchHeaders: Record<string, string> = {}
  for (const [key, value] of Object.entries(signed.headers)) {
    if (key.toLowerCase() !== 'host') {
      fetchHeaders[key] = String(value)
    }
  }

  const response = await fetch(fullUrl, {
    method,
    headers: fetchHeaders,
    body: body ?? undefined,
  })

  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `Request failed: ${response.status}`)
  }

  return response
}
