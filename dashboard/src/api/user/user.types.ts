/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * User API types
 */

import type { AwsCredentials } from '@/stores/auth.store'

/**
 * Response from /user/creds endpoint
 * Contains temporary AWS credentials
 */
export interface UserCredsResponse {
  access_key_id: string
  secret_access_key: string
  session_token: string
  expiration: number
}

/**
 * Map the wire response onto the shape the auth store holds.
 * Shared so the initial fetch and the background session keeper cannot drift.
 */
export function toAwsCredentials(response: UserCredsResponse): AwsCredentials {
  return {
    accessKeyId: response.access_key_id,
    secretAccessKey: response.secret_access_key,
    sessionToken: response.session_token,
    expiration: response.expiration,
  }
}
