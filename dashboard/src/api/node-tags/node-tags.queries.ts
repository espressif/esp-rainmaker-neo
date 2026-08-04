/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useAuthStore } from '@/stores/auth.store'
import { nodeTagsApi } from './node-tags.api'
import type { NodeTagsResponse, UpdateNodeTagsRequest } from './node-tags.types'

export const nodeTagsKeys = {
  all: ['admin', 'node-tags'] as const,
  detail: (nodeId: string) => [...nodeTagsKeys.all, nodeId] as const,
}

export function useNodeTags(nodeId: string) {
  const credentials = useAuthStore((s) => s.credentials)

  return useQuery<NodeTagsResponse, Error>({
    queryKey: nodeTagsKeys.detail(nodeId),
    queryFn: () => nodeTagsApi.getTags(nodeId),
    enabled: !!nodeId && !!credentials,
  })
}

export function useUpdateNodeTags(nodeId: string) {
  const queryClient = useQueryClient()

  return useMutation<void, Error, UpdateNodeTagsRequest>({
    mutationFn: (data) => nodeTagsApi.updateTags(nodeId, data),
    onSuccess: () => {
      return queryClient.invalidateQueries({
        queryKey: nodeTagsKeys.detail(nodeId),
      })
    },
  })
}
