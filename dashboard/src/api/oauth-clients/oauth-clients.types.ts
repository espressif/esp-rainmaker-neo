/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Admin OAuth client registry types (GET /v1/admin/clients).
 */

export type OAuthClientType = 'public' | 'confidential'

/**
 * A registered OAuth/OIDC client as returned by List.
 */
export interface OAuthClient {
  client_id: string
  client_name?: string
  client_type?: OAuthClientType
  redirect_uris?: string[]
  grant_types?: string[]
  scopes?: string[]
  require_pkce?: boolean
  /** Plaintext; present only when the list request set `get_secret=true` (confidential clients). */
  client_secret?: string
  created_at?: number
  updated_at?: number
}

/**
 * Response from GET /v1/admin/clients
 */
export interface ListOAuthClientsResponse {
  clients?: OAuthClient[]
}

/**
 * Body of PUT /v1/admin/clients/{client_id}.
 *
 * The endpoint is a **full replace** of the mutable fields, so an omitted field resets to
 * empty — callers must send the client's complete desired state, not a patch. `client_id`,
 * `client_type` and the secret are immutable and rejected outright if present.
 */
export interface UpdateOAuthClientRequest {
  /** Required by the API even when unchanged. */
  client_name: string
  redirect_uris: string[]
  grant_types: string[]
  scopes: string[]
  require_pkce?: boolean
}
