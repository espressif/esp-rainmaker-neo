/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface Grant {
  label: string
  detail?: string
}

const IDENTITY: Grant = { label: 'Identity', detail: 'Your Espressif account ID' }

const GRANTS: Record<string, Grant> = {
  openid: IDENTITY,
  email: { label: 'Email', detail: 'Your email address' },
  phone: { label: 'Phone', detail: 'Your phone number' },
  profile: { label: 'Profile', detail: 'Your name, language and profile picture' },
  'account:sessions': { label: 'Devices', detail: 'See and sign out the devices you are signed in on' },
}

/** The scope that asks for a grant no sign-out ends (OIDC Core 1.0 §11). */
export const OFFLINE_ACCESS = 'offline_access'

/** What offline_access grants, closing the list on its own because it is how long access lasts, not what the app reads: 'Alexa' -> Stay connected, Alexa keeps working while you're signed out. */
export function stayConnected(appName: string): Grant {
  return { label: 'Stay connected', detail: `${appName} keeps working while you're signed out. Disconnect anytime.` }
}

/** One line per grant on the consent screen: ['openid', 'email'] -> Identity, Email; an unknown scope is listed by its own name. */
export function describeScopes(scopes: string[]): Grant[] {
  const grants = [IDENTITY, ...scopes.filter(scope => scope !== OFFLINE_ACCESS).map(scope => GRANTS[scope] ?? { label: scope })]
  return grants.filter((grant, i) => grants.findIndex(g => g.label === grant.label) === i)
}
