/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * "Connect Amazon account" for automatic Alexa setup: Login with Amazon consent in a popup,
 * returning the one-time authorization code for the backend to redeem. Skill management at
 * Amazon is only granted on a person's behalf, so this consent click cannot be skipped.
 */

/** Dashboard route Amazon redirects the popup back to. Must be an Allowed Return URL of the security profile. */
export const LWA_CALLBACK_PATH = '/alexa-lwa-callback'

/**
 * The callback reports over a channel rather than `window.opener`: Amazon's pages may set
 * Cross-Origin-Opener-Policy, which severs the opener link for good, while any same-origin
 * context can still reach a BroadcastChannel.
 */
export const LWA_CALLBACK_CHANNEL = 'esp-rainmaker-alexa-lwa'

const LWA_AUTHORIZE_URL = 'https://www.amazon.com/ap/oa'
const LWA_SCOPE = 'alexa::ask:skills:readwrite alexa::ask:skills:test'

/** Amazon's authorization codes expire after five minutes, so waiting longer cannot succeed. */
const LWA_CONSENT_TIMEOUT_MS = 5 * 60 * 1000

export type LwaConnectFailure = 'popupBlocked' | 'denied' | 'timeout' | 'noCode' | 'superseded'

export class LwaConnectError extends Error {
  constructor(
    readonly reason: LwaConnectFailure,
    readonly detail?: string,
  ) {
    super(detail ?? reason)
    this.name = 'LwaConnectError'
  }
}

/** What the callback page posts: the authorization response's query parameters. */
export interface LwaCallbackMessage {
  state: string | null
  code: string | null
  error: string | null
  error_description: string | null
}

export function getLwaRedirectUri(): string {
  return `${window.location.origin}${LWA_CALLBACK_PATH}`
}

export function buildLwaAuthorizeUrl(clientId: string, redirectUri: string, state: string): string {
  const query = new URLSearchParams({
    client_id: clientId,
    scope: LWA_SCOPE,
    response_type: 'code',
    redirect_uri: redirectUri,
    state,
  })
  return `${LWA_AUTHORIZE_URL}?${query.toString()}`
}

export function readLwaCallback(params: URLSearchParams): LwaCallbackMessage {
  return {
    state: params.get('state'),
    code: params.get('code'),
    error: params.get('error'),
    error_description: params.get('error_description'),
  }
}

/**
 * Returns the code, throws for a response to this request that carries none, and returns
 * `null` for a response to some other request, which is not ours to act on.
 * `{state: "s1", code: "c"}` with expected "s1" -> "c"; with expected "s2" -> null.
 */
export function evaluateLwaCallback(message: LwaCallbackMessage, expectedState: string): string | null {
  if (message.state !== expectedState) {
    return null
  }
  if (message.error) {
    throw new LwaConnectError('denied', message.error_description ?? message.error)
  }
  if (!message.code) {
    throw new LwaConnectError('noCode')
  }
  return message.code
}

let cancelPending: (() => void) | null = null

/**
 * Opens Amazon's consent page in a popup and resolves with the authorization code and the
 * redirect URI it was issued for, which the backend must send back unchanged. Starting a new
 * request rejects the previous one with `superseded`.
 */
export function requestLwaAuthorizationCode(clientId: string): Promise<{ code: string; redirectUri: string }> {
  cancelPending?.()

  const redirectUri = getLwaRedirectUri()
  const state = crypto.randomUUID()
  const popup = window.open(
    buildLwaAuthorizeUrl(clientId, redirectUri, state),
    'esp-rainmaker-alexa-lwa',
    'popup=yes,width=520,height=720',
  )
  if (!popup) {
    return Promise.reject(new LwaConnectError('popupBlocked'))
  }

  return new Promise((resolve, reject) => {
    const channel = new BroadcastChannel(LWA_CALLBACK_CHANNEL)
    const finish = (settle: () => void) => {
      clearTimeout(timer)
      channel.close()
      cancelPending = null
      settle()
    }
    const timer = setTimeout(() => finish(() => reject(new LwaConnectError('timeout'))), LWA_CONSENT_TIMEOUT_MS)
    cancelPending = () => finish(() => reject(new LwaConnectError('superseded')))

    channel.onmessage = (event: MessageEvent<LwaCallbackMessage>) => {
      try {
        const code = evaluateLwaCallback(event.data, state)
        if (code) {
          finish(() => resolve({ code, redirectUri }))
        }
      } catch (error) {
        finish(() => reject(error))
      }
    }
  })
}
