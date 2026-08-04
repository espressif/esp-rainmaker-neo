/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { httpClient } from '@/api/http-client'
import { ApiError } from '@/api/api.errors'
import type {
  ListOAuthClientsResponse,
  OAuthClient,
  UpdateOAuthClientRequest,
} from './oauth-clients.types'

const ENDPOINTS = {
  clients: '/v1/admin/clients',
  client: (clientId: string) => `/v1/admin/clients/${encodeURIComponent(clientId)}`,
} as const

/**
 * Admin OAuth client registry. Goes through `httpClient`, which targets the ESP User
 * API and attaches the admin Bearer id token the Cognito authorizer expects — so an
 * expired token is refreshed rather than surfacing here.
 */
export const oauthClientsApi = {
  /**
   * `getSecret` opts into each confidential client's plaintext secret. That is the only
   * way to read a secret back: it is deliberately kept out of the published runtime
   * config document, which is anonymously readable.
   */
  list: async (options?: { getSecret?: boolean }): Promise<ListOAuthClientsResponse> => {
    try {
      const res = await httpClient.get<ListOAuthClientsResponse>(ENDPOINTS.clients, {
        params: options?.getSecret ? { get_secret: 'true' } : undefined,
      })
      return res.data
    } catch (error) {
      throw ApiError.fromAxiosError(error)
    }
  },

  /**
   * Replaces a client's mutable fields wholesale — the body is the complete desired state, so
   * anything left out is erased. Callers must build the body from a fresh read of the client;
   * never from a partial or assumed shape.
   */
  update: async (
    clientId: string,
    body: UpdateOAuthClientRequest,
  ): Promise<OAuthClient> => {
    try {
      const res = await httpClient.put<OAuthClient>(ENDPOINTS.client(clientId), body)
      return res.data
    } catch (error) {
      throw ApiError.fromAxiosError(error)
    }
  },
} as const
