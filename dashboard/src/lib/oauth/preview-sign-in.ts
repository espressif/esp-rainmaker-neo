/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * "Preview sign-in page" — opens the real end-user sign-in page so an admin sees exactly what
 * their users see.
 *
 * The published Authorization URI is an OAuth endpoint, not a page: opened bare it answers
 * `invalid_request`. Rendering the sign-in page means sending it a complete authorization
 * request — `response_type=code`, a registered client, a registered `redirect_uri`, a scope and
 * an S256 PKCE challenge, which the endpoint requires.
 *
 * The client used is the voice-assistant client, the one already registered for this deployment.
 * It is *confidential*, so the returned code is never redeemed — see `oauth-preview`.
 */

import { oauthClientsApi, type OAuthClient } from '@/api/oauth-clients'
import { generatePkceParams } from '@/lib/auth'
import { getAuthorizeUrl, getVaClientId } from '@/lib/config'

/** Dashboard route that receives the authorization response. */
const PREVIEW_CALLBACK_PATH = '/oauth-preview'

/**
 * Throwaway data, but it has to cross tabs: the sign-in page is opened with `noopener`, and a
 * browsing context with no opener does not inherit a copy of `sessionStorage`, so the callback
 * tab would find nothing there. `localStorage` is the only store both tabs see. It is kept
 * throwaway by construction instead — removed the moment the callback reads it, and refused
 * after {@link PREVIEW_REQUEST_TTL_MS} so an abandoned entry cannot be picked up later.
 */
const PREVIEW_REQUEST_STORAGE_KEY = 'esp-rainmaker-oauth-preview-request'

/** Comfortably longer than a sign-in takes, far shorter than "until this browser is reinstalled". */
const PREVIEW_REQUEST_TTL_MS = 15 * 60 * 1000

/** What a sign-in preview wants to exercise: an OIDC login with the usual identity claims. */
const PREVIEW_SCOPES = ['openid', 'email', 'profile']

interface StoredPreviewRequest {
  state: string
  code_verifier: string
  created_at: number
}

/**
 * Computed from the live origin rather than configured, so the preview works from localhost
 * during development and from the deployed dashboard without a second registration.
 */
function getPreviewRedirectUri(): string {
  return `${window.location.origin}${PREVIEW_CALLBACK_PATH}`
}

function storePreviewRequest(request: StoredPreviewRequest): void {
  localStorage.setItem(PREVIEW_REQUEST_STORAGE_KEY, JSON.stringify(request))
}

/**
 * Returns `null` when nothing was stored, the entry is unreadable, or it has aged out — all of
 * which mean the response cannot be verified.
 */
export function readPreviewRequest(): StoredPreviewRequest | null {
  try {
    const raw = localStorage.getItem(PREVIEW_REQUEST_STORAGE_KEY)
    if (!raw) {
      return null
    }
    const parsed = JSON.parse(raw) as Partial<StoredPreviewRequest>
    if (!parsed.state || typeof parsed.created_at !== 'number') {
      return null
    }
    if (Date.now() - parsed.created_at > PREVIEW_REQUEST_TTL_MS) {
      return null
    }
    return {
      state: parsed.state,
      code_verifier: parsed.code_verifier ?? '',
      created_at: parsed.created_at,
    }
  } catch {
    return null
  }
}

export function clearPreviewRequest(): void {
  try {
    localStorage.removeItem(PREVIEW_REQUEST_STORAGE_KEY)
  } catch {
    // A storage failure must not mask the outcome the callback page is showing.
  }
}

/**
 * `PUT` replaces a client wholesale, and this client's registered redirect URIs are live
 * voice-assistant account-linking callbacks. So a write is only ever built from a read that
 * looks like a whole client: `grant_types` is non-empty on every client the registry can
 * produce, and its absence means the record cannot be trusted as the basis of a full replace.
 * (`redirect_uris` is omitted from the response when empty, so absence there is genuinely
 * "none registered yet" — nothing an added URI can destroy.)
 */
function assertReplaceable(client: OAuthClient): asserts client is OAuthClient & {
  grant_types: string[]
} {
  if (!Array.isArray(client.grant_types) || client.grant_types.length === 0) {
    throw new Error(
      `Refusing to update client "${client.client_id}": the registry returned an incomplete record.`,
    )
  }
}

/**
 * Makes this origin's callback a registered redirect URI of the client, writing only when it is
 * actually missing and only ever as the existing set plus ours. The write is read back, so a
 * redirect URI that failed to survive the replace is reported instead of going unnoticed.
 * Returns the client, whose registered scopes shape the request built below.
 */
async function ensurePreviewRedirectUri(
  clientId: string,
  redirectUri: string,
): Promise<OAuthClient> {
  // No `get_secret`: nothing in this flow reads, holds or sends the client's secret.
  const { clients } = await oauthClientsApi.list()
  const client = clients?.find(entry => entry.client_id === clientId)
  if (!client) {
    throw new Error(`OAuth client "${clientId}" is not registered for this deployment.`)
  }

  assertReplaceable(client)

  const registered = client.redirect_uris ?? []
  if (registered.includes(redirectUri)) {
    return client
  }

  const desired = [...registered, redirectUri]
  const updated = await oauthClientsApi.update(clientId, {
    client_name: client.client_name ?? clientId,
    redirect_uris: desired,
    grant_types: client.grant_types,
    scopes: client.scopes ?? [],
    require_pkce: client.require_pkce,
  })

  const survived = updated.redirect_uris ?? []
  const lost = desired.filter(uri => !survived.includes(uri))
  if (lost.length > 0) {
    throw new Error(
      `Client "${clientId}" came back without these redirect URIs: ${lost.join(', ')}. Check its account-linking configuration.`,
    )
  }

  return client
}

/**
 * The authorization endpoint refuses a scope the client is not registered for, so the request
 * asks only for what this client actually allows.
 */
function previewScope(client: OAuthClient): string {
  const allowed = client.scopes ?? []
  const wanted = PREVIEW_SCOPES.filter(scope => allowed.includes(scope))
  return (wanted.length > 0 ? wanted : allowed).join(' ')
}

function buildAuthorizeRequestUrl(params: {
  authorizeUrl: string
  clientId: string
  redirectUri: string
  scope: string
  state: string
  codeChallenge: string
}): string {
  const query = new URLSearchParams({
    response_type: 'code',
    client_id: params.clientId,
    redirect_uri: params.redirectUri,
    scope: params.scope,
    state: params.state,
    code_challenge: params.codeChallenge,
    // The endpoint accepts no other method; `plain` is rejected.
    code_challenge_method: 'S256',
  })
  return `${params.authorizeUrl}?${query.toString()}`
}

/**
 * Registers this origin's callback if needed, then opens the sign-in page in a new tab.
 * Rejects with a message worth showing when the client cannot be prepared.
 */
export async function openPreviewSignIn(): Promise<void> {
  const authorizeUrl = getAuthorizeUrl()
  const clientId = getVaClientId()
  if (!authorizeUrl || !clientId) {
    throw new Error('This deployment has not published an authorization endpoint yet.')
  }

  const redirectUri = getPreviewRedirectUri()
  const client = await ensurePreviewRedirectUri(clientId, redirectUri)

  const { verifier, challenge, state } = await generatePkceParams()
  storePreviewRequest({ state, code_verifier: verifier, created_at: Date.now() })

  window.open(
    buildAuthorizeRequestUrl({
      authorizeUrl,
      clientId,
      redirectUri,
      scope: previewScope(client),
      state,
      codeChallenge: challenge,
    }),
    '_blank',
    'noopener,noreferrer',
  )
}
