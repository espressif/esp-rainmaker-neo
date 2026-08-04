/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { integrationsApi, pushIntegrationsApi } from './integrations.api'
import type { AlexaConfigGetResponse, AlexaConfigRequest, AlexaConfigResponse } from './integrations.types'
import type { GvaConfigGetResponse, GvaConfigRequest, GvaConfigResponse, GvaConfigDeleteResponse } from './integrations.types'
import type { PushIntegrationType, PushIntegrationRequest, ListIntegrationsResponse, RegisterIntegrationResponse, IntegrationStatusResponse } from './integrations.types'

/**
 * Query key factory for integrations domain
 */
export const integrationsKeys = {
  all: ['integrations'] as const,
  alexa: () => [...integrationsKeys.all, 'alexa'] as const,
  alexaConfig: () => [...integrationsKeys.alexa(), 'config'] as const,
  gva: () => [...integrationsKeys.all, 'gva'] as const,
  gvaConfig: () => [...integrationsKeys.gva(), 'config'] as const,
}

/**
 * Query options factory for integrations domain
 */
export const integrationsQueries = {
  alexaConfig: () => ({
    queryKey: integrationsKeys.alexaConfig(),
    queryFn: integrationsApi.getAlexaConfig,
    staleTime: 1000 * 60 * 5, // 5 minutes
    retry: 1,
  }),
}

/**
 * Hook for fetching Alexa integration configuration
 */
export function useGetAlexaConfig(options?: { enabled?: boolean }) {
  return useQuery<AlexaConfigGetResponse, Error>({
    ...integrationsQueries.alexaConfig(),
    enabled: options?.enabled ?? true,
  })
}

/**
 * Hook for configuring Alexa integration
 */
export function useConfigureAlexa() {
  const queryClient = useQueryClient()

  return useMutation<AlexaConfigResponse, Error, AlexaConfigRequest>({
    mutationFn: integrationsApi.configureAlexa,
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: integrationsKeys.alexaConfig() })
    },
  })
}

/**
 * Query options factory for GVA domain
 */
export const gvaQueries = {
  config: () => ({
    queryKey: integrationsKeys.gvaConfig(),
    queryFn: integrationsApi.getGvaConfig,
    staleTime: 1000 * 60 * 5,
    retry: 1,
  }),
}

/**
 * Hook for fetching GVA integration configuration
 */
export function useGetGvaConfig(options?: { enabled?: boolean }) {
  return useQuery<GvaConfigGetResponse, Error>({
    ...gvaQueries.config(),
    enabled: options?.enabled ?? true,
  })
}

/**
 * Hook for configuring GVA integration
 */
export function useConfigureGva() {
  const queryClient = useQueryClient()

  return useMutation<GvaConfigResponse, Error, GvaConfigRequest>({
    mutationFn: integrationsApi.configureGva,
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: integrationsKeys.gvaConfig() })
    },
  })
}

/**
 * Hook for deleting GVA integration configuration
 */
export function useDeleteGvaConfig() {
  const queryClient = useQueryClient()

  return useMutation<GvaConfigDeleteResponse, Error, void>({
    mutationFn: integrationsApi.deleteGvaConfig,
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: integrationsKeys.gvaConfig() })
    },
  })
}

/**
 * Query keys for push integrations
 */
export const pushIntegrationsKeys = {
  list: () => [...integrationsKeys.all, 'push', 'list'] as const,
}

/**
 * Hook for listing all integrations (the page filters to push types)
 */
export function usePushIntegrations() {
  return useQuery<ListIntegrationsResponse, Error>({
    queryKey: pushIntegrationsKeys.list(),
    queryFn: pushIntegrationsApi.list,
    staleTime: 1000 * 60 * 5,
    retry: 1,
  })
}

/**
 * Hook for registering a push integration (APNS / APNS_SANDBOX / GCM)
 */
export function useRegisterPushIntegration() {
  const queryClient = useQueryClient()

  return useMutation<RegisterIntegrationResponse, Error, { integrationType: PushIntegrationType; data: PushIntegrationRequest }>({
    mutationFn: ({ integrationType, data }) => pushIntegrationsApi.register(integrationType, data),
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: pushIntegrationsKeys.list() })
    },
  })
}

/**
 * Hook for updating a push integration's credentials
 */
export function useUpdatePushIntegration() {
  const queryClient = useQueryClient()

  return useMutation<IntegrationStatusResponse, Error, { integrationId: string; data: PushIntegrationRequest }>({
    mutationFn: ({ integrationId, data }) => pushIntegrationsApi.update(integrationId, data),
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: pushIntegrationsKeys.list() })
    },
  })
}

/**
 * Hook for deleting a push integration
 */
export function useDeletePushIntegration() {
  const queryClient = useQueryClient()

  return useMutation<IntegrationStatusResponse, Error, string>({
    mutationFn: (integrationId) => pushIntegrationsApi.remove(integrationId),
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: pushIntegrationsKeys.list() })
    },
  })
}
