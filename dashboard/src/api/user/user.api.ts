/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import axios from "axios";
import { getApiGatewayUrl } from "@/lib/config";
import { getAccessToken, getIdToken } from "@/lib/auth";
import type { UserCredsResponse } from "./user.types";

const ENDPOINTS = {
  creds: "/v1/user/credentials",
} as const;

/**
 * User API functions
 * Uses axios directly (not shared httpClient) because:
 * - Base URL is getApiGatewayUrl() (from the fetched client-outputs / config.json)
 * - Differs from httpClient base URL (ESP User API)
 * - This endpoint needs both tokens, unlike every other one
 */
export const userApi = {
  /**
   * Fetch temporary AWS credentials.
   *
   * Sends the access token as Bearer authorization (the method declares API Gateway
   * authorization scopes, so the Cognito authorizer validates that) and the ID token
   * in the body, which the Cognito Identity Pool exchange requires. The server checks
   * both came from the same sign-in, so they must be from one login.
   */
  getCreds: async (): Promise<UserCredsResponse> => {
    const baseUrl = getApiGatewayUrl()
    const accessToken = getAccessToken()
    const idToken = getIdToken()

    if (!baseUrl) {
      throw new Error('API Gateway URL is not configured')
    }

    if (!accessToken || !idToken) {
      throw new Error('Both an access token and an ID token are required')
    }

    const url = `${baseUrl.replace(/\/$/, '')}${ENDPOINTS.creds}`
    const response = await axios.post(url, { id_token: idToken }, {
      headers: {
        Authorization: `Bearer ${accessToken}`,
        'Content-Type': 'application/json',
      },
    })
    
    return response.data as UserCredsResponse
  },
} as const;
