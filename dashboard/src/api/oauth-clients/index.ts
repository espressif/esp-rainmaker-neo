/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Admin OAuth clients API module exports
 */

export type {
  OAuthClient,
  OAuthClientType,
  ListOAuthClientsResponse,
  UpdateOAuthClientRequest,
} from './oauth-clients.types'

export { oauthClientsApi } from './oauth-clients.api'

export {
  oauthClientsKeys,
  oauthClientsQueries,
  useOAuthClients,
  useOAuthClient,
} from './oauth-clients.queries'
