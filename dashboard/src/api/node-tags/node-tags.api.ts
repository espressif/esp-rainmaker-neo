/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { signedFetch } from '@/api/signed-fetch'
import type { NodeTagsResponse, UpdateNodeTagsRequest } from './node-tags.types'

const ENDPOINTS = {
  tags: (nodeId: string) => `/v1/admin/nodes/${encodeURIComponent(nodeId)}/tags`,
} as const

export const nodeTagsApi = {
  getTags: async (nodeId: string): Promise<NodeTagsResponse> => {
    const response = await signedFetch('GET', ENDPOINTS.tags(nodeId))
    return response.json() as Promise<NodeTagsResponse>
  },

  updateTags: async (nodeId: string, data: UpdateNodeTagsRequest): Promise<void> => {
    await signedFetch('PUT', ENDPOINTS.tags(nodeId), JSON.stringify(data))
  },
} as const
