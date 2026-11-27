/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describeError, type OAuthErrorBody, type ShownError } from './errors.ts'
import { contactFields, normalizeUsername } from '../lib/contact.ts'

export type Result = { ok: true; data: { redirect_to?: string } } | { ok: false; error: ShownError }

// Paths are relative so the API Gateway stage prefix carries over.
async function post(path: string, body: unknown): Promise<Result> {
  let response: Response
  try {
    response = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  } catch {
    return { ok: false, error: describeError(null) }
  }
  // A body that is not JSON, such as a gateway's own error page, reads as empty.
  const data = (await response.json().catch(() => null)) ?? {}
  return response.ok ? { ok: true, data } : { ok: false, error: describeError(data as OAuthErrorBody) }
}

/** finish posts a step that completes a sign-in, whose success must name where to go; one naming nowhere is the generic error. */
async function finish(path: string, body: unknown): Promise<Result> {
  const result = await post(path, body)
  return result.ok && !result.data.redirect_to ? { ok: false, error: describeError(null) } : result
}

/** The RP-Initiated Logout 1.0 s2 parameters /oauth2/logout carries to the sign-out page. */
const LOGOUT_PARAMS = ['id_token_hint', 'logout_hint', 'client_id', 'post_logout_redirect_uri', 'state']

// Addresses go out normalized, as contactFields sends them: the server matches them exactly, and a pasted one often ends in a space or a typed phone number has spaces and dashes.
export const api = {
  // flowID is the sign-in this page was drawn for, so the server can refuse a page that a newer sign-in replaced.
  signIn: (username: string, password: string, provider: string, flowID?: string) =>
    finish('login/password', { username: normalizeUsername(username), password, provider, flow_id: flowID }),

  // Sending again for the same sign-in replaces its code and keeps the count of wrong attempts.
  sendCode: (username: string, flowID?: string) =>
    post('../v1/auth/otp/initiate', { username: normalizeUsername(username), flow_id: flowID }),

  verifyCode: (otp: string, flowID?: string) =>
    finish('../v1/auth/otp/verify', { otp, flow_id: flowID }),

  answerConsent: (decision: 'allow' | 'deny', flowID?: string) =>
    finish('consent', { decision, flow_id: flowID }),

  // Sends back the sign-out parameters this page was opened with, in the body so id_token_hint stays out of the request URL; an empty redirect_to means nowhere validated.
  confirmLogout: (csrfToken: string) => {
    const opened = new URLSearchParams(window.location.search)
    const params = Object.fromEntries(LOGOUT_PARAMS.flatMap(name => opened.has(name) ? [[name, opened.get(name)]] : []))
    return post('logout', { ...params, csrf_token: csrfToken })
  },
}

export const account = {
  // Sign-up takes email or phone_number, never username; signing up an unverified account again resends its code.
  signUp: (username: string, password: string, provider?: string) =>
    post('../v1/user/auth/signup', { ...contactFields(username), password, provider }),

  verifySignup: (username: string, code: string, provider?: string) =>
    post('../v1/user/auth/signup/verify', { ...contactFields(username), code, provider }),

  startRecovery: (username: string, provider?: string) =>
    post('../v1/user/auth/password-recovery', { username: normalizeUsername(username), provider }),

  confirmRecovery: (username: string, code: string, password: string, provider?: string) =>
    post('../v1/user/auth/password-recovery/confirmation', { username: normalizeUsername(username), code, new_password: password, provider }),
}
