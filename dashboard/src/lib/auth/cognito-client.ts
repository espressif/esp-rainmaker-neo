/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Shared Cognito Identity Provider client for admin authentication.
 * Admins authenticate directly against the admin AWS Cognito user pool
 * (public SPA app client — no secret, so no SECRET_HASH is required).
 */

import { CognitoIdentityProviderClient } from '@aws-sdk/client-cognito-identity-provider'
import { getAwsRegion } from '../config'

let client: CognitoIdentityProviderClient | null = null
let clientRegion: string | null = null

/**
 * Built on first use, not at import: the region comes from the runtime config,
 * which is fetched after this module loads. A client constructed at import time
 * would pin an empty region and send every sign-in to the wrong endpoint, where
 * the app client does not exist. Rebuilt if the region ever changes.
 */
export function getCognitoClient(): CognitoIdentityProviderClient {
  const region = getAwsRegion()
  if (!client || clientRegion !== region) {
    // No automatic retries. Cognito auth sessions are single-use, so an SDK retry of a
    // challenge call can never succeed — it reports "session can only be used once"
    // (NotAuthorizedException) and masks the real failure; the SDK even classifies
    // Cognito's LimitExceededException as retryable throttling, turning a rate limit
    // into that misleading error. Every caller has its own retry story anyway: the
    // interactive flows retry via the user, the session keeper via its own backoff.
    client = new CognitoIdentityProviderClient({ region, maxAttempts: 1 })
    clientRegion = region
  }
  return client
}
