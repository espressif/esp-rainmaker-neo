/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface NodeTagsResponse {
  admin: Record<string, string>
  device: Record<string, string>
  user: Record<string, string>
}

export interface UpdateNodeTagsRequest {
  admin?: Record<string, string | null>
  user?: Record<string, string | null>
}
