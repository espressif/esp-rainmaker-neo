/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * OAuth helpers the dashboard uses outside the admin's own Cognito session (`@/lib/auth`):
 * the end-user IdP's sign-in preview, and Login with Amazon for automatic Alexa setup.
 */

export {
  clearPreviewRequest,
  openPreviewSignIn,
  readPreviewRequest,
} from './preview-sign-in'

export {
  getLwaRedirectUri,
  LWA_CALLBACK_CHANNEL,
  LwaConnectError,
  readLwaCallback,
  requestLwaAuthorizationCode,
} from './lwa-connect'
