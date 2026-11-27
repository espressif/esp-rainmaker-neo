/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** RFC 6749 §5.2 shape, which every ESP User endpoint answers errors in. */
export interface OAuthErrorBody {
  error?: string
  error_description?: string
  /** The account endpoints answer `{"message"}`: a sentence written for the person, unlike error_description. */
  message?: string
}

/** What the page does next; every code has one, so no error is a dead end. */
type ErrorAction =
  | 'retry'
  | 'restart'
  | 'replaced'     // a newer sign-in in this browser took over; Start again would follow it, so none is offered
  | 'send-code'    // the account needs verifying first

export interface ShownError {
  message: string
  action: ErrorAction
  /** Error demands action; warning is the sign-in still being possible by another route. */
  severity: 'error' | 'warning'
}

// Codes are the contract and the words are ours: error_description is for operators and is never shown.
const ERROR_COPY: Record<string, ShownError> = {
  // The server gives a wrong password and an unknown account this one code, so the form cannot test whether an address is registered.
  invalid_credentials: { message: 'That email or phone number and password don’t match. Try again, or reset your password.', action: 'retry', severity: 'error' },
  unconfirmed_account: { message: 'Verify your account to finish setting it up.', action: 'send-code', severity: 'warning' },
  // No endpoint answers Cognito's new-password challenge, so no screen here can set it.
  new_password_required: {
    message: 'This account needs a new password before you can sign in. Contact support to set one.',
    action: 'retry', severity: 'warning',
  },
  // Only a right password reaches this, and Cognito's forgot password clears it.
  password_reset_required: { message: 'Your password was reset. Use Forgot password to set a new one.', action: 'retry', severity: 'warning' },
  provider_not_allowed: { message: 'This app does not allow signing in this way.', action: 'retry', severity: 'error' },
  access_denied: { message: 'This account can’t be signed in. Contact support.', action: 'retry', severity: 'error' },
  expired_flow: { message: 'This sign-in took too long.', action: 'restart', severity: 'error' },
  flow_replaced: { message: 'You started another sign-in in this browser.', action: 'replaced', severity: 'error' },
  invalid_code: { message: 'That code is wrong.', action: 'retry', severity: 'error' },
  expired_code: { message: 'This code has expired. Send a new one.', action: 'retry', severity: 'error' },
  code_locked: { message: 'Too many wrong codes.', action: 'restart', severity: 'error' },
  delivery_failed: { message: 'We couldn’t send a code there. Check the address and try again.', action: 'retry', severity: 'error' },
  invalid_request: { message: 'Something was missing from that request.', action: 'restart', severity: 'error' },
  invalid_client: { message: 'This application is not set up correctly.', action: 'restart', severity: 'error' },
  too_many_requests: { message: 'Too many attempts. Wait a moment and try again.', action: 'retry', severity: 'error' },
}

/** endsSignIn says the sign-in this page was drawn for is over, so the page shows its end instead of the form. */
export const endsSignIn = (error: ShownError) => error.action === 'restart' || error.action === 'replaced'

// The server's sentence for these is written for whoever integrates the app ("response_type must be code."), not for the person.
const TERMINAL_HEADINGS: Record<string, string> = {
  invalid_request: 'This sign-in can’t continue',
  invalid_client: 'This sign-in can’t continue',
  server_error: 'Something went wrong on our side',
}

/** terminalHeading heads a sign-in that cannot continue: ours for an integrator's sentence, which the screen keeps underneath, else the server's own. */
export function terminalHeading(code: string | undefined, message: string): string {
  return (code && TERMINAL_HEADINGS[code]) || message
}

const UNKNOWN: ShownError = { message: 'Something went wrong. Please try again.', action: 'retry', severity: 'error' }

/** describeError maps a response to what the person reads; null means the request never completed, and an unknown code still offers a retry. */
export function describeError(body: OAuthErrorBody | null): ShownError {
  if (!body) return UNKNOWN
  if (body.error) return ERROR_COPY[body.error] ?? UNKNOWN
  const message = body.message?.trim()
  if (!message) return UNKNOWN
  return { message: message.length > 200 ? UNKNOWN.message : message, action: 'retry', severity: 'error' }
}
