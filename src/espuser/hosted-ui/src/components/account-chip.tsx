/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** The signed-in account a question is about; nothing when the server could not read it. */
export function AccountChip({ account }: { account?: string }) {
  if (!account) return null
  return (
    <p class="consent-account">
      <span class="consent-avatar" aria-hidden="true">{account.charAt(0).toUpperCase()}</span>
      {account}
    </p>
  )
}
