/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { SignatureV4 } from '@smithy/signature-v4'
import { HttpRequest } from '@smithy/protocol-http'
import { Sha256 } from '@aws-crypto/sha256-js'
import { useAuthStore } from '@/stores/auth.store'
import { getAwsRegion } from '@/lib/config'

interface Sigv4RequestOptions {
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  url: string
  body?: unknown
}

/**
 * Make a SigV4-signed HTTP request to API Gateway (IAM auth).
 * Uses temporary AWS credentials from the auth store.
 */
export async function sigv4Request<T>({ method, url, body }: Sigv4RequestOptions): Promise<T> {
  const { credentials, isCredentialsValid } = useAuthStore.getState()

  if (!credentials) {
    throw new Error('No AWS credentials available')
  }

  if (!isCredentialsValid()) {
    useAuthStore.getState().clearCredentials()
    throw new Error('AWS credentials expired')
  }

  const parsed = new URL(url)
  const headers: Record<string, string> = {
    host: parsed.hostname,
    'content-type': 'application/json',
  }

  const serializedBody = body != null ? JSON.stringify(body) : undefined

  const request = new HttpRequest({
    method,
    protocol: parsed.protocol,
    hostname: parsed.hostname,
    port: parsed.port ? Number(parsed.port) : undefined,
    path: parsed.pathname,
    query: Object.fromEntries(parsed.searchParams.entries()),
    headers,
    body: serializedBody,
  })

  const signer = new SignatureV4({
    service: 'execute-api',
    region: getAwsRegion(),
    credentials: {
      accessKeyId: credentials.accessKeyId,
      secretAccessKey: credentials.secretAccessKey,
      sessionToken: credentials.sessionToken,
    },
    sha256: Sha256,
  })

  const signed = await signer.sign(request)

  const response = await fetch(url, {
    method,
    headers: signed.headers,
    body: serializedBody,
  })

  if (!response.ok) {
    const errorBody = await response.text()
    throw new Error(`Request failed with status code ${response.status}: ${errorBody}`)
  }

  return response.json() as Promise<T>
}
